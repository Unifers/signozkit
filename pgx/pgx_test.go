package pgx

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTracerInstallsOnAPoolConfig(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://user@localhost:5432/db")
	if err != nil {
		t.Fatal(err)
	}

	cfg.ConnConfig.Tracer = Tracer()
	if cfg.ConnConfig.Tracer == nil {
		t.Fatal("Tracer returned nothing to install")
	}
}
