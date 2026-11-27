package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"billetera.local/ms-usuarios/internal/config"
	"billetera.local/ms-usuarios/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		// Connection and SQL errors can contain private connection information.
		fmt.Fprintln(os.Stderr, "No se pudieron aplicar las migraciones; revisa conexión y compatibilidad del esquema.")
		os.Exit(1)
	}
	fmt.Println("Migraciones de Usuarios aplicadas y verificadas")
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := config.LoadDatabase()
	if err != nil {
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	return migrations.Apply(ctx, pool)
}
