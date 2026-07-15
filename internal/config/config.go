// Package config loads webhook configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host                 string
	Org                  string
	Environment          string
	PolicyNames          []string
	Token                string
	RequireDigestPinning bool
	DenyUnknownArtifacts bool
	CacheTTL             time.Duration
	CertFile             string
	KeyFile              string
	ListenAddr           string
	ShutdownDelay        time.Duration
	LogLevel             slog.Level
	LogFormat            string // "json" or "text"
}

func Load() (*Config, error) {
	c := &Config{
		Host:                 getenv("KOSLI_HOST", "https://app.kosli.com"),
		Org:                  os.Getenv("KOSLI_ORG"),
		Environment:          os.Getenv("KOSLI_ENVIRONMENT"),
		Token:                os.Getenv("KOSLI_API_TOKEN"),
		RequireDigestPinning: getenvBool("REQUIRE_DIGEST_PINNING", true),
		DenyUnknownArtifacts: getenvBool("DENY_UNKNOWN_ARTIFACTS", true),
		CertFile:             getenv("TLS_CERT_FILE", "/certs/tls.crt"),
		KeyFile:              getenv("TLS_KEY_FILE", "/certs/tls.key"),
		ListenAddr:           getenv("LISTEN_ADDR", ":8443"),
		LogFormat:            getenv("LOG_FORMAT", "json"),
	}

	if raw := os.Getenv("KOSLI_POLICY_NAMES"); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.PolicyNames = append(c.PolicyNames, p)
			}
		}
	}

	ttl, err := time.ParseDuration(getenv("CACHE_TTL", "60s"))
	if err != nil {
		return nil, fmt.Errorf("invalid CACHE_TTL: %w", err)
	}
	c.CacheTTL = ttl

	delay, err := time.ParseDuration(getenv("SHUTDOWN_DELAY", "5s"))
	if err != nil {
		return nil, fmt.Errorf("invalid SHUTDOWN_DELAY: %w", err)
	}
	c.ShutdownDelay = delay

	switch strings.ToLower(getenv("LOG_LEVEL", "info")) {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn", "warning":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return nil, fmt.Errorf("invalid LOG_LEVEL (want debug|info|warn|error)")
	}

	if c.Org == "" || c.Token == "" {
		return nil, fmt.Errorf("KOSLI_ORG and KOSLI_API_TOKEN are required")
	}
	if c.Environment != "" && len(c.PolicyNames) > 0 {
		return nil, fmt.Errorf("KOSLI_ENVIRONMENT and KOSLI_POLICY_NAMES are mutually exclusive")
	}
	if c.Environment == "" && len(c.PolicyNames) == 0 {
		return nil, fmt.Errorf("one of KOSLI_ENVIRONMENT or KOSLI_POLICY_NAMES must be set")
	}
	return c, nil
}

// Logger builds the process-wide structured logger.
func (c *Config) Logger() *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	var h slog.Handler
	if c.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}

// Scope describes the assertion scope for logging.
func (c *Config) Scope() string {
	if len(c.PolicyNames) > 0 {
		return "policies=" + strings.Join(c.PolicyNames, ",")
	}
	return "environment=" + c.Environment
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getenvBool(k string, d bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return d
	}
	return b
}
