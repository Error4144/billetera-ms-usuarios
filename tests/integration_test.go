package tests

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	pb "billetera.local/ms-usuarios/gen/billetera/usuarios/v1"
	"billetera.local/ms-usuarios/internal/rpc"
	"billetera.local/ms-usuarios/internal/security"
	"billetera.local/ms-usuarios/internal/users"
	"billetera.local/ms-usuarios/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/emptypb"
)

// Each test uses a fresh schema in an explicitly designated test database.
func database(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("USUARIOS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("define USUARIOS_TEST_DATABASE_URL para PostgreSQL real")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("configuración de base de pruebas inválida")
	}
	if cfg.ConnConfig.Database != "billetera_usuarios_test" {
		t.Fatal("se requiere la base aislada billetera_usuarios_test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("no se pudo abrir el pool de pruebas")
	}
	var entropy [12]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	schema := "test_" + hex.EncodeToString(entropy[:])
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		admin.Close()
		t.Fatal("no se pudo crear esquema de pruebas")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := admin.Exec(cleanup, "DROP SCHEMA "+identifier+" CASCADE")
		admin.Close()
		if err != nil {
			t.Error("no se pudo limpiar el esquema temporal de pruebas")
		}
	})
	return pool
}

func TestMigrationsConcurrentAndChecksum(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if err := migrations.Check(ctx, pool); err == nil {
		t.Fatal("Check aceptó base sin migraciones")
	}
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- migrations.Apply(ctx, pool) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 2 {
		t.Fatalf("versiones: %d, error: %v", count, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET checksum='alterado'"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Check(ctx, pool); err == nil {
		t.Fatal("Check aceptó checksum alterado")
	}
	if err := migrations.Apply(ctx, pool); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatal("no se detectó la migración modificada")
	}
}

func TestRegistrationThroughGRPCAndPostgres(t *testing.T) {
	pool := database(t)
	if err := migrations.Apply(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Check(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "certs")
	command := exec.Command("go", "run", "../cmd/devcert", "--out", directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("certificados de prueba: %v, %s", err, output)
	}
	srv, err := rpc.New(users.New(pool), pool, rpc.TLSFiles{Certificate: filepath.Join(directory, "server.crt"), Key: filepath.Join(directory, "server.key"), CA: filepath.Join(directory, "ca.crt")})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(listener) }()
	t.Cleanup(func() { srv.Stop(); <-done })
	creds, err := rpc.ClientCredentials(rpc.TLSFiles{Certificate: filepath.Join(directory, "bff.crt"), Key: filepath.Join(directory, "bff.key"), CA: filepath.Join(directory, "ca.crt")}, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewUsersServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request := &pb.RegisterUserRequest{Email: " GRPC@Example.test ", Password: "una frase de contraseña para gRPC", DisplayName: " Registro gRPC "}
	result, err := client.RegisterUser(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetProfile().GetEmail() != "grpc@example.test" || result.GetPreferences().GetCurrency() != "CLP" || result.GetProfile().GetId() == "" {
		t.Fatal("respuesta gRPC incorrecta")
	}
	encoded, err := protojson.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), request.Password) || strings.Contains(string(encoded), "argon2id") {
		t.Fatal("credenciales filtradas por gRPC")
	}
	if _, err = client.RegisterUser(ctx, request); status.Code(err) != codes.AlreadyExists {
		t.Fatal("duplicado no traducido a gRPC")
	}
	if _, err = client.RegisterUser(ctx, &pb.RegisterUserRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatal("entrada inválida no traducida")
	}
	logged, err := client.Login(ctx, &pb.LoginRequest{Email: request.Email, Password: request.Password})
	if err != nil {
		t.Fatal(err)
	}
	auth := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+logged.GetSessionToken()))
	if session, err := client.ValidateSession(auth, &emptypb.Empty{}); err != nil || session.GetUserId() != result.GetProfile().GetId() {
		t.Fatal("sesión gRPC inválida")
	}
	profile, err := client.GetProfile(auth, &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	profile, err = client.UpdateProfile(auth, &pb.UpdateProfileRequest{DisplayName: "Perfil real gRPC", ExpectedVersion: profile.GetVersion()})
	if err != nil || profile.GetDisplayName() != "Perfil real gRPC" {
		t.Fatal("edición gRPC de perfil falló")
	}
	prefs, err := client.GetPreferences(auth, &emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	prefs, err = client.UpdatePreferences(auth, &pb.UpdatePreferencesRequest{ExpectedVersion: prefs.GetVersion(), Currency: "CLP", TimeZone: "UTC", MicroExpenseMaximumAmount: 9007199254740993, MicroExpenseMinimumCount: 4})
	if err != nil || prefs.GetMicroExpenseMaximumAmount() != 9007199254740993 {
		t.Fatal("edición gRPC de preferencias falló")
	}
	if _, err = client.GetProfile(ctx, &emptypb.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("perfil sin sesión accesible")
	}
	if _, err = client.UpdateProfile(auth, &pb.UpdateProfileRequest{DisplayName: "versión antigua", ExpectedVersion: 1}); status.Code(err) != codes.Aborted {
		t.Fatal("conflicto gRPC incorrecto")
	}
	duplicateMetadata := metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer "+logged.GetSessionToken(), "authorization", "Bearer "+logged.GetSessionToken()))
	if _, err = client.ValidateSession(duplicateMetadata, &emptypb.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("se aceptaron metadatos duplicados")
	}
	if _, err = client.Logout(auth, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Logout(auth, &emptypb.Empty{}); err != nil {
		t.Fatal("logout gRPC repetido falló")
	}
	if _, err = client.ValidateSession(auth, &emptypb.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("sesión gRPC siguió activa")
	}
	for _, table := range []string{"usuarios", "preferencias_usuario", "outbox_eventos"} {
		var count int
		expected := 1
		if table == "outbox_eventos" {
			expected = 2
		}
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != expected {
			t.Fatalf("registro gRPC incoherente en %s", table)
		}
	}
}

func TestMigrationRefusesExistingSchema(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE TABLE usuarios (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, pool); err == nil {
		t.Fatal("se aceptó un esquema incompatible")
	}
	var missing bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NULL").Scan(&missing); err != nil || !missing {
		t.Fatal("la migración fallida dejó estado parcial")
	}
}

func TestRegisterAtomicAndConcurrent(t *testing.T) {
	pool := database(t)
	ctx := context.Background()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	service := users.New(pool)
	input := users.RegisterInput{Email: " Camila@Example.test ", Name: " Camila ", Password: "una contraseña larga para pruebas"}
	registered, err := service.Register(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if registered.Profile.Email != "camila@example.test" || registered.Profile.Name != "Camila" || registered.Preferences.MaximumAmount != 3000 || registered.Preferences.Version != 1 {
		t.Fatal("perfil o preferencias incorrectos")
	}
	var hash string
	if err = pool.QueryRow(ctx, "SELECT hash_contrasena FROM usuarios WHERE id=$1", registered.Profile.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash == input.Password || !security.VerifyPassword(input.Password, hash) {
		t.Fatal("contraseña no protegida correctamente")
	}
	var payload string
	if err = pool.QueryRow(ctx, "SELECT datos::text FROM outbox_eventos WHERE usuario_id=$1 AND tipo='UsuarioRegistrado' AND publicado_en IS NULL", registered.Profile.ID).Scan(&payload); err != nil || payload != "{}" {
		t.Fatal("falta evento pendiente mínimo")
	}
	if _, err = service.Register(ctx, input); !errors.Is(err, users.ErrEmailExists) {
		t.Fatal("correo duplicado no detectado")
	}

	input.Email = "concurrente@example.test"
	results := make(chan error, 4)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := service.Register(ctx, input); results <- err }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, users.ErrEmailExists) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("altas concurrentes exitosas: %d", successes)
	}
	for _, table := range []string{"usuarios", "preferencias_usuario", "outbox_eventos"} {
		var count int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 2 {
			t.Fatalf("estado incoherente en %s: %d, %v", table, count, err)
		}
	}

	// Force the final step to fail: identity and preferences must roll back too.
	if _, err = pool.Exec(ctx, "ALTER TABLE outbox_eventos ADD CONSTRAINT fallo_prueba CHECK (false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	input.Email = "rollback@example.test"
	if _, err = service.Register(ctx, input); err == nil {
		t.Fatal("se ignoró el fallo de outbox")
	}
	var partial int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM usuarios WHERE email=$1", input.Email).Scan(&partial); err != nil || partial != 0 {
		t.Fatal("quedó un usuario parcial")
	}
}

func TestRegisterRejectsInvalidInput(t *testing.T) {
	service := users.New(nil) // Invalid input must be rejected before touching storage.
	for _, input := range []users.RegisterInput{
		{Email: "sin-correo", Name: "Camila", Password: "una contraseña larga"},
		{Email: "a@example.test", Name: " ", Password: "una contraseña larga"},
		{Email: "a@example.test", Name: "Camila", Password: "corta"},
	} {
		if _, err := service.Register(context.Background(), input); !errors.Is(err, users.ErrInvalid) {
			t.Fatal("entrada inválida no rechazada")
		}
	}
}
