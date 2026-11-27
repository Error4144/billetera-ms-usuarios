package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"billetera.local/ms-usuarios/internal/config"
	"billetera.local/ms-usuarios/internal/rpc"
	"billetera.local/ms-usuarios/internal/users"
	"billetera.local/ms-usuarios/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	check := flag.Bool("check-config", false, "validar variables sin iniciar servidor")
	probe := flag.Bool("healthcheck", false, "comprobar disponibilidad gRPC con certificado local de salud")
	flag.Parse()
	if *probe {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, "Usuarios no disponible")
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		fmt.Println("Configuración de Usuarios válida")
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err = run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "No se pudo iniciar Usuarios: revisa PostgreSQL, migraciones y certificados TLS")
		os.Exit(1)
	}
}
func run(ctx context.Context, cfg config.Config) error {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(startup, cfg.Postgres)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(startup); err != nil {
		return err
	}
	if err = migrations.Check(startup, pool); err != nil {
		return err
	}
	service, err := users.NewWithPolicy(pool, users.SessionPolicy{TTL: cfg.SessionTTL, Idle: cfg.SessionIdle})
	if err != nil {
		return err
	}
	server, err := rpc.New(service, pool, rpc.TLSFiles{Certificate: os.Getenv("GRPC_TLS_CERT_FILE"), Key: os.Getenv("GRPC_TLS_KEY_FILE"), CA: os.Getenv("GRPC_TLS_CA_FILE")})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		return err
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Printf("Usuarios gRPC escuchando en :%d con TLS mutuo\n", cfg.GRPCPort)
	select {
	case err := <-done:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	case <-ctx.Done():
		stopped := make(chan struct{})
		go func() { server.GracefulStop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			server.Stop()
			<-stopped
		}
		return nil
	}
}
func healthcheck() error {
	creds, err := rpc.ClientCredentials(rpc.TLSFiles{Certificate: os.Getenv("GRPC_HEALTH_CERT_FILE"), Key: os.Getenv("GRPC_HEALTH_KEY_FILE"), CA: os.Getenv("GRPC_TLS_CA_FILE")}, "localhost")
	if err != nil {
		return err
	}
	port := os.Getenv("GRPC_PORT")
	if port == "" {
		port = "50051"
	}
	conn, err := grpc.NewClient("127.0.0.1:"+port, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: rpc.ServiceName})
	if err != nil {
		return err
	}
	if result.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return errors.New("not serving")
	}
	return nil
}
