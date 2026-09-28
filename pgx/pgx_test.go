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

func TestSpanNameUsesTheQueryName(t *testing.T) {
	cases := []struct {
		name string
		stmt string
		want string
	}{
		{
			name: "sqlc header",
			stmt: "-- name: ContactsDue :many\nSELECT id FROM contacts\nWHERE next_run_at < now()",
			want: "ContactsDue",
		},
		{
			name: "sqlc header after a blank line",
			stmt: "\n\n-- name: CampaignForSend :one\nSELECT 1",
			want: "CampaignForSend",
		},
		{
			name: "plain sql keeps its first line",
			stmt: "SELECT id FROM contacts\nWHERE id = $1",
			want: "SELECT id FROM contacts",
		},
		{
			name: "a comment that is not sqlc's is skipped",
			stmt: "-- recompute the lease\nUPDATE contacts SET next_run_at = now()",
			want: "UPDATE contacts SET next_run_at = now()",
		},
		{
			name: "a comment with no name falls through to the sql",
			stmt: "-- name:\nSELECT 1",
			want: "SELECT 1",
		},
		{
			name: "only a comment",
			stmt: "-- nothing here",
			want: "-- nothing here",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := spanName(c.stmt); got != c.want {
				t.Errorf("spanName = %q, want %q", got, c.want)
			}
		})
	}
}
