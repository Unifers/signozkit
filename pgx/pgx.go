// Package pgx instruments a pgx v5 connection pool, so every query becomes a
// span under the request or job that ran it.
package pgx

import (
	"strings"

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
	return otelpgx.NewTracer(append([]otelpgx.Option{otelpgx.WithSpanNameFunc(spanName)}, opts...)...)
}

// spanName names a query's span after the query.
//
// sqlc writes the name into a leading comment — "-- name: ContactsDue :many" —
// and otelpgx names a span from the statement's first line, which for generated
// code is always that comment. Every query in the service then arrives in SigNoz
// as a span called "--", and a trace shows which calls were slow without showing
// which queries they were.
//
// Anything else falls back to the first line of actual SQL, which is what
// otelpgx would have done on its own.
func spanName(stmt string) string {
	for line := range strings.Lines(stmt) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if name, ok := sqlcName(line); ok {
			return name
		}
		if strings.HasPrefix(line, "--") {
			continue
		}
		return line
	}
	return stmt
}

// sqlcName reads the query name out of sqlc's header comment, without the
// ":one" or ":many" that says what shape the result is: two queries differing
// only in that are still two queries, and the name alone identifies them.
func sqlcName(line string) (string, bool) {
	rest, ok := strings.CutPrefix(line, "--")
	if !ok {
		return "", false
	}
	rest, ok = strings.CutPrefix(strings.TrimSpace(rest), "name:")
	if !ok {
		return "", false
	}

	name, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
	return name, name != ""
}
