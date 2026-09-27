package signozkit

import (
	"os"
	"strings"
)

// Config says who the service is and where its telemetry goes. Build one with
// [FromEnv] and adjust fields before passing it to [SetupConfig], or let
// [Setup] do both.
type Config struct {
	// ServiceName is service.name on every record and span — the name the
	// service is listed under in SigNoz. Names are case-sensitive there.
	ServiceName string

	// Environment is deployment.environment.name, e.g. "production".
	Environment string

	// Endpoint is the collector's OTLP/HTTP base URL, e.g.
	// "https://collector.example.com" or "http://signoz-otel-collector:4318".
	// The /v1/logs and /v1/traces paths are appended for you. Empty means
	// nothing is exported.
	Endpoint string

	// Headers are sent with every export, as comma-separated key=value pairs
	// (OTEL_EXPORTER_OTLP_HEADERS). SigNoz Cloud needs
	// "signoz-ingestion-key=<key>"; a self-hosted collector needs nothing.
	Headers string

	// ResourceAttributes are extra comma-separated key=value labels stamped on
	// every record and span (OTEL_RESOURCE_ATTRIBUTES). ServiceName and
	// Environment win where they collide.
	ResourceAttributes string

	// Enabled is the kill switch. False keeps logging to stdout and exports
	// nothing, whatever the other fields say.
	Enabled bool

	// LogLevel is the minimum level logged: debug, info, warn or error.
	LogLevel string

	// LocalEnvironments are the Environment values treated as a developer
	// machine: human-readable text logs and no export, so local runs never
	// show up under the deployed service in SigNoz.
	LocalEnvironments []string
}

// DefaultLocalEnvironments are the environments that neither export nor log
// JSON unless Config.LocalEnvironments says otherwise.
var DefaultLocalEnvironments = []string{"local", "development"}

// FromEnv reads the configuration from the environment:
//
//	OTEL_SERVICE_NAME            service name (default: service)
//	ENVIRONMENT                  deployment environment (default: "local")
//	OTEL_EXPORTER_OTLP_ENDPOINT  collector base URL; empty = no export
//	OTEL_EXPORTER_OTLP_HEADERS   extra export headers, key=value,key=value
//	OTEL_RESOURCE_ATTRIBUTES     extra labels, key=value,key=value
//	SIGNOZ_ENABLED               "false" disables export (default: true)
//	LOG_LEVEL                    debug | info | warn | error (default: info)
//
// service is the name used when OTEL_SERVICE_NAME is unset.
func FromEnv(service string) Config {
	return Config{
		ServiceName:        getenv("OTEL_SERVICE_NAME", service),
		Environment:        getenv("ENVIRONMENT", "local"),
		Endpoint:           getenv("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		Headers:            getenv("OTEL_EXPORTER_OTLP_HEADERS", ""),
		ResourceAttributes: getenv("OTEL_RESOURCE_ATTRIBUTES", ""),
		Enabled:            !strings.EqualFold(getenv("SIGNOZ_ENABLED", "true"), "false"),
		LogLevel:           getenv("LOG_LEVEL", "info"),
		LocalEnvironments:  DefaultLocalEnvironments,
	}
}

// IsLocal reports whether Environment is one of LocalEnvironments.
func (c Config) IsLocal() bool {
	for _, env := range c.LocalEnvironments {
		if strings.EqualFold(c.Environment, env) {
			return true
		}
	}
	return false
}

// Exporting reports whether this configuration sends anything to the
// collector: not local, enabled, and an endpoint set.
func (c Config) Exporting() bool {
	return !c.IsLocal() && c.Enabled && c.Endpoint != ""
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// keyValues parses the comma-separated key=value form the OTLP list variables
// use. A malformed pair is skipped rather than fatal: one bad label is not a
// reason to refuse to start.
func keyValues(raw string) map[string]string {
	out := make(map[string]string)
	for _, field := range strings.Split(raw, ",") {
		key, value, ok := strings.Cut(field, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	return out
}
