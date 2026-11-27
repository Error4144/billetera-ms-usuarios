package rpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "billetera.local/ms-usuarios/gen/billetera/usuarios/v1"
	"billetera.local/ms-usuarios/internal/users"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeRegistrar struct {
	err      error
	panicNow bool
}

func (f fakeRegistrar) Login(context.Context, string, string) (users.LoginResult, error) {
	return users.LoginResult{}, users.ErrUnauthenticated
}
func (f fakeRegistrar) ValidateSession(context.Context, string) (users.Session, error) {
	return users.Session{}, users.ErrUnauthenticated
}
func (f fakeRegistrar) Logout(context.Context, string) error { return users.ErrUnauthenticated }
func (f fakeRegistrar) GetProfile(context.Context, string) (users.Profile, error) {
	return users.Profile{}, users.ErrUnauthenticated
}
func (f fakeRegistrar) UpdateProfile(context.Context, string, string, int64) (users.Profile, error) {
	return users.Profile{}, users.ErrUnauthenticated
}
func (f fakeRegistrar) GetPreferences(context.Context, string) (users.Preferences, error) {
	return users.Preferences{}, users.ErrUnauthenticated
}
func (f fakeRegistrar) UpdatePreferences(context.Context, string, users.Preferences, int64) (users.Preferences, error) {
	return users.Preferences{}, users.ErrUnauthenticated
}

func (f fakeRegistrar) Register(context.Context, users.RegisterInput) (users.Registration, error) {
	if f.panicNow {
		panic("SQL secret")
	}
	return users.Registration{Profile: users.Profile{ID: "id", Email: "a@example.test", Name: "Nombre", Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}, Preferences: users.Preferences{Currency: "CLP", TimeZone: "America/Santiago", MaximumAmount: 3000, MinimumCount: 3, Version: 1, UpdatedAt: time.Now()}}, f.err
}

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

// Independent certificate fixtures exercise verified TLS chains and URI authorization.
func certificates(t *testing.T) (TLSFiles, map[string]TLSFiles) {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(dir, "ca.crt")
	if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	all := map[string]TLSFiles{}
	for i, identity := range []struct{ name, uri string }{{"server", ""}, {"bff", BFFIdentity}, {"health", HealthIdentity}, {"intruder", "spiffe://billetera/otro"}} {
		leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{SerialNumber: big.NewInt(int64(i + 2)), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
		if identity.name == "server" {
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			leaf.DNSNames = []string{"localhost"}
		} else {
			u, err := url.Parse(identity.uri)
			if err != nil {
				t.Fatal(err)
			}
			leaf.URIs = []*url.URL{u}
			leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		signed, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		private, err := x509.MarshalPKCS8PrivateKey(leafKey)
		if err != nil {
			t.Fatal(err)
		}
		paths := TLSFiles{Certificate: filepath.Join(dir, identity.name+".crt"), Key: filepath.Join(dir, identity.name+".key"), CA: caPath}
		if err = os.WriteFile(paths.Certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: signed}), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(paths.Key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
			t.Fatal(err)
		}
		all[identity.name] = paths
	}
	return all["server"], all
}
func liveServer(t *testing.T, registrar Registrar, db Pinger) (string, map[string]TLSFiles) {
	t.Helper()
	serverFiles, clients := certificates(t)
	srv, err := New(registrar, db, serverFiles)
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
	return listener.Addr().String(), clients
}
func connect(t *testing.T, address string, files TLSFiles) *grpc.ClientConn {
	t.Helper()
	creds, err := ClientCredentials(files, "localhost")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func TestLiveTLSAuthorization(t *testing.T) {
	address, clients := liveServer(t, fakeRegistrar{}, fakeDB{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bff := connect(t, address, clients["bff"])
	result, err := pb.NewUsersServiceClient(bff).RegisterUser(ctx, &pb.RegisterUserRequest{})
	if err != nil || result.GetProfile().GetId() != "id" || result.GetPreferences().GetCurrency() != "CLP" {
		t.Fatalf("registro: %v", err)
	}
	if _, err = pb.NewUsersServiceClient(bff).Login(ctx, &pb.LoginRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("login sin credenciales debe rechazarse")
	}
	if _, err = pb.NewUsersServiceClient(bff).GetProfile(ctx, &emptypb.Empty{}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("perfil sin sesión debe rechazarse")
	}
	healthClient := connect(t, address, clients["health"])
	ready, err := healthpb.NewHealthClient(healthClient).Check(ctx, &healthpb.HealthCheckRequest{Service: ServiceName})
	if err != nil || ready.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Fatal("salud no disponible")
	}
	if _, err = pb.NewUsersServiceClient(healthClient).RegisterUser(ctx, &pb.RegisterUserRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("identidad de salud pudo registrar")
	}
	intruder := connect(t, address, clients["intruder"])
	if _, err = pb.NewUsersServiceClient(intruder).RegisterUser(ctx, &pb.RegisterUserRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("servicio ajeno pudo registrar")
	}
	// A client trusting the server CA but lacking a client certificate cannot connect.
	data, err := os.ReadFile(clients["bff"].CA)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(data)
	noCert, err := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "localhost"})))
	if err != nil {
		t.Fatal(err)
	}
	defer noCert.Close()
	if _, err = healthpb.NewHealthClient(noCert).Check(ctx, &healthpb.HealthCheckRequest{}); err == nil {
		t.Fatal("se aceptó cliente sin certificado")
	}
}
func TestErrorsAreMappedWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{{users.ErrInvalid, codes.InvalidArgument}, {users.ErrEmailExists, codes.AlreadyExists}, {context.Canceled, codes.Canceled}, {context.DeadlineExceeded, codes.DeadlineExceeded}, {errors.New("SQL password=secret"), codes.Internal}} {
		h := &handler{registrar: fakeRegistrar{err: tc.err}, slots: make(chan struct{}, 1)}
		_, err := h.RegisterUser(context.Background(), &pb.RegisterUserRequest{})
		if status.Code(err) != tc.code || strings.Contains(err.Error(), "secret") {
			t.Fatalf("error incorrecto: %v", err)
		}
	}
	h := &handler{registrar: fakeRegistrar{}, slots: make(chan struct{}, 1)}
	h.slots <- struct{}{}
	if _, err := h.RegisterUser(context.Background(), &pb.RegisterUserRequest{}); status.Code(err) != codes.ResourceExhausted {
		t.Fatal("no se limitó concurrencia")
	}
	h = &handler{registrar: fakeRegistrar{}, slots: make(chan struct{}, 1)}
	if _, err := h.RegisterUser(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatal("nil no rechazado")
	}
	if err := authorize(context.Background(), "rpc"); status.Code(err) != codes.Unauthenticated {
		t.Fatal("contexto sin identidad aceptado")
	}
}
func TestHealthFailureAndPanicRecovery(t *testing.T) {
	address, clients := liveServer(t, fakeRegistrar{panicNow: true}, fakeDB{err: errors.New("database down")})
	conn := connect(t, address, clients["bff"])
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil || result.GetStatus() != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Fatal("se anunció salud sin base disponible")
	}
	if _, err = healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: "desconocido"}); status.Code(err) != codes.NotFound {
		t.Fatal("servicio desconocido aceptado")
	}
	if _, err = pb.NewUsersServiceClient(conn).RegisterUser(ctx, &pb.RegisterUserRequest{}); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "secret") {
		t.Fatal("panic filtrado")
	}
	if _, err = New(fakeRegistrar{}, fakeDB{}, TLSFiles{}); err == nil {
		t.Fatal("servidor aceptó configuración TLS vacía")
	}
}
