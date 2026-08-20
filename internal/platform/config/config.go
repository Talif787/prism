// Package config loads and validates runtime configuration from the environment,
// following twelve-factor principles. All configuration is explicit and typed;
// there are no hidden defaults that differ between environments.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

type Environment string

const (
	EnvLocal      Environment = "local"
	EnvDev        Environment = "dev"
	EnvStaging    Environment = "staging"
	EnvProduction Environment = "production"
)

type AuthMode string

const (
	AuthModeDev  AuthMode = "dev"
	AuthModeOIDC AuthMode = "oidc"
)

type Config struct {
	Env      Environment
	HTTP     HTTPConfig
	Log      LogConfig
	DB       DatabaseConfig
	Auth     AuthConfig
	OTel     OTelConfig
	Internal InternalConfig
}

type HTTPConfig struct {
	Host            string
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

func (h HTTPConfig) Addr() string { return fmt.Sprintf("%s:%d", h.Host, h.Port) }

type LogConfig struct {
	Level  string
	Format string
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	ConnMaxLifetime time.Duration
}

type AuthConfig struct {
	Mode           AuthMode
	DevHS256Secret string
	OIDCIssuer     string
	OIDCAudience   string
}

type InternalConfig struct {
	APIToken string
}

type OTelConfig struct {
	Enabled      bool
	ServiceName  string
	OTLPEndpoint string
	SamplerRatio float64
}

// Load reads configuration from the environment and validates it. It returns an
// error listing every problem found, so operators can fix configuration in one pass.
func Load() (Config, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	env := Environment(getStr("APP_ENV", string(EnvLocal)))

	cfg := Config{
		Env: env,
		HTTP: HTTPConfig{
			Host:            getStr("HTTP_HOST", "0.0.0.0"),
			Port:            getInt("HTTP_PORT", 8080),
			ReadTimeout:     getDur("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: getDur("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
		},
		Log: LogConfig{
			Level:  getStr("LOG_LEVEL", "info"),
			Format: getStr("LOG_FORMAT", "json"),
		},
		DB: DatabaseConfig{
			URL:             getStr("DATABASE_URL", ""),
			MaxConns:        getInt32("DATABASE_MAX_CONNS", 20),
			MinConns:        getInt32("DATABASE_MIN_CONNS", 2),
			ConnMaxLifetime: getDur("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Auth: AuthConfig{
			Mode:           AuthMode(getStr("AUTH_MODE", string(AuthModeDev))),
			DevHS256Secret: getStr("AUTH_DEV_HS256_SECRET", ""),
			OIDCIssuer:     getStr("AUTH_OIDC_ISSUER", ""),
			OIDCAudience:   getStr("AUTH_OIDC_AUDIENCE", "prism-console"),
		},
		Internal: InternalConfig{
			APIToken: getStr("INTERNAL_API_TOKEN", ""),
		},
		OTel: OTelConfig{
			Enabled:      getBool("OTEL_ENABLED", true),
			ServiceName:  getStr("OTEL_SERVICE_NAME", "prism-controlplane"),
			OTLPEndpoint: getStr("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
			SamplerRatio: getFloat("OTEL_TRACES_SAMPLER_RATIO", 1.0),
		},
	}

	if cfg.DB.URL == "" {
		add("DATABASE_URL is required")
	}
	if cfg.Internal.APIToken == "" {
		add("INTERNAL_API_TOKEN is required")
	}
	switch cfg.Auth.Mode {
	case AuthModeDev:
		if cfg.Env == EnvProduction {
			add("AUTH_MODE=dev is not permitted in production")
		}
		if cfg.Auth.DevHS256Secret == "" {
			add("AUTH_DEV_HS256_SECRET is required when AUTH_MODE=dev")
		}
	case AuthModeOIDC:
		if cfg.Auth.OIDCIssuer == "" {
			add("AUTH_OIDC_ISSUER is required when AUTH_MODE=oidc")
		}
	default:
		add("AUTH_MODE must be one of: dev, oidc")
	}
	if cfg.HTTP.Port <= 0 || cfg.HTTP.Port > 65535 {
		add("HTTP_PORT must be between 1 and 65535")
	}
	if cfg.OTel.SamplerRatio < 0 || cfg.OTel.SamplerRatio > 1 {
		add("OTEL_TRACES_SAMPLER_RATIO must be between 0.0 and 1.0")
	}

	if len(problems) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

func getStr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// getInt32 reads a bounded positive integer, clamping to the int32 range so the
// conversion is provably safe (satisfies gosec G115). Values are small pool sizes.
func getInt32(key string, def int32) int32 {
	v := getInt(key, int(def))
	if v < 1 {
		return def
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}

func getFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func getBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func getDur(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// ErrValidation is returned when configuration fails validation.
var ErrValidation = errors.New("configuration validation failed")
