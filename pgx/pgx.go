// Package pgx instruments a pgx v5 connection pool, so every query becomes a
// span under the request or job that ran it.
package pgx

import (
	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
)

// Tracer returns the query tracer to install on a pool's configuration:
//
//	cfg, err := pgxpool.ParseConfig(dsn)
//	if err != nil {
//		return err
//	}
//	cfg.ConnConfig.Tracer = signozpgx.Tracer()
//
// Span names are the SQL trimmed to a single line, which is otelpgx's own
// default, so a trace list stays readable instead of carrying whole queries in
// it. Query parameters are left out: they are the values a statement ran
// against, which for most services means customer data. Pass
// otelpgx.WithIncludeQueryParameters where those values are safe to keep.
//
// Queries reach it only through a context that holds the calling span, which
// pgx takes on every method whose name ends in Context.
func Tracer(opts ...otelpgx.Option) pgx.QueryTracer {
	return otelpgx.NewTracer(opts...)
}
