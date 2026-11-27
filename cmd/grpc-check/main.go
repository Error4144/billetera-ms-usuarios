// grpc-check verifies the live endpoint without creating development users.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	pb "billetera.local/ms-usuarios/gen/billetera/usuarios/v1"
	"billetera.local/ms-usuarios/internal/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func main() {
	address := flag.String("address", "127.0.0.1:50051", "endpoint de Usuarios")
	directory := flag.String("certs", ".local-certs", "certificados locales del BFF")
	flag.Parse()
	if err := check(*address, *directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("gRPC verificado: salud, validación de registro y rechazo de login sin credenciales")
}
func check(address, directory string) error {
	creds, err := rpc.ClientCredentials(rpc.TLSFiles{Certificate: filepath.Join(directory, "bff.crt"), Key: filepath.Join(directory, "bff.key"), CA: filepath.Join(directory, "ca.crt")}, "localhost")
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	health, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: rpc.ServiceName})
	if err != nil {
		return fmt.Errorf("salud gRPC: %w", err)
	}
	if health.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("PostgreSQL no disponible")
	}
	client := pb.NewUsersServiceClient(conn)
	if _, err = client.RegisterUser(ctx, &pb.RegisterUserRequest{}); status.Code(err) != codes.InvalidArgument {
		return fmt.Errorf("se esperaba INVALID_ARGUMENT en registro vacío")
	}
	if _, err = client.Login(ctx, &pb.LoginRequest{}); status.Code(err) != codes.Unauthenticated {
		return fmt.Errorf("se esperaba UNAUTHENTICATED en login vacío")
	}
	return nil
}
