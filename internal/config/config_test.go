package config

import (
	"testing"
	"time"
)

func environment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"POSTGRES_HOST": "127.0.0.1", "POSTGRES_PORT": "55431",
		"POSTGRES_DB": "billetera_usuarios", "POSTGRES_USER": "app_usuarios",
		"POSTGRES_PASSWORD": "contraseña@:/?#de-prueba", "POSTGRES_SSLMODE": "disable",
		"GRPC_PORT": "50051", "KAFKA_BROKERS": "kafka:9092, kafka-2:9092",
	} {
		t.Setenv(key, value)
	}
	t.Setenv("SESSION_TTL", "24h")
	t.Setenv("SESSION_IDLE_TIMEOUT", "30m")
}

func TestSessionPolicy(t *testing.T) {
	environment(t)
	cfg, err := Load()
	if err != nil || cfg.SessionTTL != 24*time.Hour || cfg.SessionIdle != 30*time.Minute {
		t.Fatal("política predeterminada incorrecta")
	}
	for _, pair := range [][2]string{{"SESSION_TTL", "0s"}, {"SESSION_TTL", "inválido"}, {"SESSION_IDLE_TIMEOUT", "-1s"}, {"SESSION_IDLE_TIMEOUT", "25h"}} {
		t.Run(pair[0]+pair[1], func(t *testing.T) {
			environment(t)
			t.Setenv(pair[0], pair[1])
			if _, err := Load(); err == nil {
				t.Fatal("política inválida aceptada")
			}
		})
	}
}

func TestDatabaseCredentialsAndDefaults(t *testing.T) {
	environment(t)
	t.Setenv("POSTGRES_SSLMODE", "")
	cfg, err := LoadDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Password != "contraseña@:/?#de-prueba" || cfg.ConnConfig.Database != "billetera_usuarios" || cfg.ConnConfig.TLSConfig == nil {
		t.Fatal("se alteraron credenciales o política TLS predeterminada")
	}
	t.Setenv("KAFKA_BROKERS", "")
	if _, err = LoadDatabase(); err != nil {
		t.Fatal("migraciones no deben depender de Kafka")
	}
	if _, err = Load(); err == nil {
		t.Fatal("configuración de servicio debe exigir Kafka")
	}
}

func TestRequiredDatabaseVariables(t *testing.T) {
	for _, key := range []string{"POSTGRES_HOST", "POSTGRES_DB", "POSTGRES_USER", "POSTGRES_PASSWORD"} {
		t.Run(key, func(t *testing.T) {
			environment(t)
			t.Setenv(key, " ")
			if _, err := LoadDatabase(); err == nil {
				t.Fatal("se aceptó variable obligatoria vacía")
			}
		})
	}
}

func TestPortsAndBrokers(t *testing.T) {
	environment(t)
	cfg, err := Load()
	if err != nil || cfg.GRPCPort != 50051 || len(cfg.KafkaBrokers) != 2 || cfg.KafkaBrokers[1] != "kafka-2:9092" {
		t.Fatal("configuración válida rechazada")
	}
	for _, key := range []string{"POSTGRES_PORT", "GRPC_PORT"} {
		for _, value := range []string{"0", "65536", "abc"} {
			t.Run(key+value, func(t *testing.T) {
				environment(t)
				t.Setenv(key, value)
				if _, err := Load(); err == nil {
					t.Fatal("se aceptó puerto inválido")
				}
			})
		}
	}
	t.Setenv("KAFKA_BROKERS", "sin-puerto")
	if _, err = Load(); err == nil {
		t.Fatal("se aceptó broker sin puerto")
	}
}
