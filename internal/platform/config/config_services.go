package config

import (
	"fmt"
	"strings"
	"time"
)

// This file adds per-service configuration loaders for the gateway and relay,
// reusing the env helpers and shared structs from config.go. Each loader
// validates only the settings its service needs, so the control plane does not
// require Kafka or Redis configuration and vice versa.

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

type KafkaConfig struct {
	Brokers                []string
	RequiredAcks           int
	AllowAutoTopicCreation bool
	TopicMetrics           string
	TopicLogs              string
	TopicTraces            string
	TopicEvents            string
}

type ControlPlaneClientConfig struct {
	VerifyURL        string
	InternalToken    string
	Timeout          time.Duration
	CacheTTL         time.Duration
	NegativeCacheTTL time.Duration
}

type LimitsConfig struct {
	RatePerSecond      int
	Burst              int
	CardinalityBudget  int64
	CardinalityWindow  time.Duration
	EnforceCardinality bool
	MaxBodyBytes       int64
}

type GatewayConfig struct {
	Env          Environment
	HTTP         HTTPConfig
	GRPC         GRPCConfig
	Log          LogConfig
	OTel         OTelConfig
	Redis        RedisConfig
	Kafka        KafkaConfig
	ControlPlane ControlPlaneClientConfig
	Limits       LimitsConfig
}

type RelayConfig struct {
	Env          Environment
	Log          LogConfig
	OTel         OTelConfig
	DB           DatabaseConfig
	Kafka        KafkaConfig
	PollInterval time.Duration
	BatchSize    int
	MaxAttempts  int
}

func loadLog() LogConfig {
	return LogConfig{Level: getStr("LOG_LEVEL", "info"), Format: getStr("LOG_FORMAT", "json")}
}

func loadOTel(defaultService string) OTelConfig {
	return OTelConfig{
		Enabled:      getBool("OTEL_ENABLED", true),
		ServiceName:  getStr("OTEL_SERVICE_NAME", defaultService),
		OTLPEndpoint: getStr("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		SamplerRatio: getFloat("OTEL_TRACES_SAMPLER_RATIO", 1.0),
	}
}

func loadKafka() KafkaConfig {
	brokers := strings.Split(getStr("KAFKA_BROKERS", "localhost:9092"), ",")
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}
	return KafkaConfig{
		Brokers:                brokers,
		RequiredAcks:           getInt("KAFKA_REQUIRED_ACKS", -1),
		AllowAutoTopicCreation: getBool("KAFKA_AUTO_CREATE_TOPICS", true),
		TopicMetrics:           getStr("KAFKA_TOPIC_METRICS", "telemetry.metrics"),
		TopicLogs:              getStr("KAFKA_TOPIC_LOGS", "telemetry.logs"),
		TopicTraces:            getStr("KAFKA_TOPIC_TRACES", "telemetry.traces"),
		TopicEvents:            getStr("KAFKA_TOPIC_EVENTS", "tenancy.events"),
	}
}

// LoadGateway reads and validates the ingest gateway configuration.
func LoadGateway() (GatewayConfig, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	cfg := GatewayConfig{
		Env: Environment(getStr("APP_ENV", string(EnvLocal))),
		HTTP: HTTPConfig{
			Host:            getStr("HTTP_HOST", "0.0.0.0"),
			Port:            getInt("HTTP_PORT", 8090),
			ReadTimeout:     getDur("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    getDur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: getDur("HTTP_SHUTDOWN_TIMEOUT", 20*time.Second),
		},
		GRPC: GRPCConfig{
			Enabled: getBool("GATEWAY_GRPC_ENABLED", true),
			Host:    getStr("GRPC_HOST", "0.0.0.0"),
			Port:    getInt("GRPC_PORT", 4317),
		},
		Log:   loadLog(),
		OTel:  loadOTel("prism-gateway"),
		Kafka: loadKafka(),
		Redis: RedisConfig{
			Addr:     getStr("REDIS_ADDR", "localhost:6379"),
			Password: getStr("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
			PoolSize: getInt("REDIS_POOL_SIZE", 10),
		},
		ControlPlane: ControlPlaneClientConfig{
			VerifyURL:        getStr("CONTROL_PLANE_VERIFY_URL", "http://localhost:8080/internal/v1/keys/verify"),
			InternalToken:    getStr("INTERNAL_API_TOKEN", ""),
			Timeout:          getDur("CONTROL_PLANE_TIMEOUT", 3*time.Second),
			CacheTTL:         getDur("AUTH_CACHE_TTL", 60*time.Second),
			NegativeCacheTTL: getDur("AUTH_NEGATIVE_CACHE_TTL", 10*time.Second),
		},
		Limits: LimitsConfig{
			RatePerSecond:      getInt("RATE_LIMIT_PER_SECOND", 50000),
			Burst:              getInt("RATE_LIMIT_BURST", 100000),
			CardinalityBudget:  int64(getInt("CARDINALITY_BUDGET", 100000)),
			CardinalityWindow:  getDur("CARDINALITY_WINDOW", 24*time.Hour),
			EnforceCardinality: getBool("CARDINALITY_ENFORCE", false),
			MaxBodyBytes:       int64(getInt("MAX_BODY_BYTES", 4*1024*1024)),
		},
	}

	if cfg.ControlPlane.InternalToken == "" {
		add("INTERNAL_API_TOKEN is required")
	}
	if len(cfg.Kafka.Brokers) == 0 || cfg.Kafka.Brokers[0] == "" {
		add("KAFKA_BROKERS is required")
	}
	if cfg.Redis.Addr == "" {
		add("REDIS_ADDR is required")
	}
	if cfg.Limits.RatePerSecond <= 0 {
		add("RATE_LIMIT_PER_SECOND must be positive")
	}
	if cfg.Limits.Burst < cfg.Limits.RatePerSecond {
		add("RATE_LIMIT_BURST must be >= RATE_LIMIT_PER_SECOND")
	}
	if len(problems) > 0 {
		return GatewayConfig{}, fmt.Errorf("invalid gateway configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// LoadRelay reads and validates the outbox relay configuration.
func LoadRelay() (RelayConfig, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	cfg := RelayConfig{
		Env:  Environment(getStr("APP_ENV", string(EnvLocal))),
		Log:  loadLog(),
		OTel: loadOTel("prism-relay"),
		DB: DatabaseConfig{
			URL:             getStr("DATABASE_URL", ""),
			MaxConns:        getInt32("DATABASE_MAX_CONNS", 5),
			MinConns:        getInt32("DATABASE_MIN_CONNS", 1),
			ConnMaxLifetime: getDur("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Kafka:        loadKafka(),
		PollInterval: getDur("RELAY_POLL_INTERVAL", 1*time.Second),
		BatchSize:    getInt("RELAY_BATCH_SIZE", 100),
		MaxAttempts:  getInt("RELAY_MAX_ATTEMPTS", 10),
	}

	if cfg.DB.URL == "" {
		add("DATABASE_URL is required")
	}
	if len(cfg.Kafka.Brokers) == 0 || cfg.Kafka.Brokers[0] == "" {
		add("KAFKA_BROKERS is required")
	}
	if cfg.BatchSize <= 0 {
		add("RELAY_BATCH_SIZE must be positive")
	}
	if len(problems) > 0 {
		return RelayConfig{}, fmt.Errorf("invalid relay configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// --- Consumer (stream consumer to ClickHouse) ---

type ClickHouseConfig struct {
	Addr         string
	Database     string
	Username     string
	Password     string
	DialTimeout  time.Duration
	MaxOpenConns int
}

type ConsumerConfig struct {
	Env          Environment
	Log          LogConfig
	OTel         OTelConfig
	HTTP         HTTPConfig
	Kafka        KafkaConfig
	ClickHouse   ClickHouseConfig
	GroupID      string
	BatchMaxRows int
	BatchMaxWait time.Duration
}

// LoadConsumer reads and validates the stream consumer configuration.
func LoadConsumer() (ConsumerConfig, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	cfg := ConsumerConfig{
		Env:  Environment(getStr("APP_ENV", string(EnvLocal))),
		Log:  loadLog(),
		OTel: loadOTel("prism-consumer"),
		HTTP: HTTPConfig{
			Host:            getStr("HTTP_HOST", "0.0.0.0"),
			Port:            getInt("HTTP_PORT", 8091),
			ReadTimeout:     getDur("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    getDur("HTTP_WRITE_TIMEOUT", 15*time.Second),
			ShutdownTimeout: getDur("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second),
		},
		Kafka: loadKafka(),
		ClickHouse: ClickHouseConfig{
			Addr:         getStr("CLICKHOUSE_ADDR", "localhost:9000"),
			Database:     getStr("CLICKHOUSE_DATABASE", "default"),
			Username:     getStr("CLICKHOUSE_USERNAME", "default"),
			Password:     getStr("CLICKHOUSE_PASSWORD", ""),
			DialTimeout:  getDur("CLICKHOUSE_DIAL_TIMEOUT", 10*time.Second),
			MaxOpenConns: getInt("CLICKHOUSE_MAX_OPEN_CONNS", 10),
		},
		GroupID:      getStr("KAFKA_CONSUMER_GROUP", "prism-consumer"),
		BatchMaxRows: getInt("CONSUMER_BATCH_MAX_ROWS", 5000),
		BatchMaxWait: getDur("CONSUMER_BATCH_MAX_WAIT", 1*time.Second),
	}

	if len(cfg.Kafka.Brokers) == 0 || cfg.Kafka.Brokers[0] == "" {
		add("KAFKA_BROKERS is required")
	}
	if cfg.ClickHouse.Addr == "" {
		add("CLICKHOUSE_ADDR is required")
	}
	if cfg.BatchMaxRows <= 0 {
		add("CONSUMER_BATCH_MAX_ROWS must be positive")
	}
	if len(problems) > 0 {
		return ConsumerConfig{}, fmt.Errorf("invalid consumer configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// --- Query service (read path over ClickHouse) ---

type QueryConfig struct {
	Env                 Environment
	Log                 LogConfig
	OTel                OTelConfig
	HTTP                HTTPConfig
	ClickHouse          ClickHouseConfig
	Redis               RedisConfig
	ControlPlane        ControlPlaneClientConfig
	NameCacheTTL        time.Duration
	MaxExecutionSeconds int
}

// LoadQuery reads and validates the query service configuration.
func LoadQuery() (QueryConfig, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	cfg := QueryConfig{
		Env:  Environment(getStr("APP_ENV", string(EnvLocal))),
		Log:  loadLog(),
		OTel: loadOTel("prism-query"),
		HTTP: HTTPConfig{
			Host:            getStr("HTTP_HOST", "0.0.0.0"),
			Port:            getInt("HTTP_PORT", 8092),
			ReadTimeout:     getDur("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    getDur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: getDur("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		ClickHouse: ClickHouseConfig{
			Addr:         getStr("CLICKHOUSE_ADDR", "localhost:9000"),
			Database:     getStr("CLICKHOUSE_DATABASE", "default"),
			Username:     getStr("CLICKHOUSE_USERNAME", "default"),
			Password:     getStr("CLICKHOUSE_PASSWORD", ""),
			DialTimeout:  getDur("CLICKHOUSE_DIAL_TIMEOUT", 10*time.Second),
			MaxOpenConns: getInt("CLICKHOUSE_MAX_OPEN_CONNS", 10),
		},
		Redis: RedisConfig{
			Addr:     getStr("REDIS_ADDR", "localhost:6379"),
			Password: getStr("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
			PoolSize: getInt("REDIS_POOL_SIZE", 10),
		},
		ControlPlane: ControlPlaneClientConfig{
			VerifyURL:        getStr("CONTROL_PLANE_VERIFY_URL", "http://localhost:8080/internal/v1/keys/verify"),
			InternalToken:    getStr("INTERNAL_API_TOKEN", ""),
			Timeout:          getDur("CONTROL_PLANE_TIMEOUT", 3*time.Second),
			CacheTTL:         getDur("AUTH_CACHE_TTL", 60*time.Second),
			NegativeCacheTTL: getDur("AUTH_NEGATIVE_CACHE_TTL", 10*time.Second),
		},
		NameCacheTTL:        getDur("QUERY_NAME_CACHE_TTL", 10*time.Second),
		MaxExecutionSeconds: getInt("QUERY_MAX_EXECUTION_SECONDS", 15),
	}

	if cfg.ClickHouse.Addr == "" {
		add("CLICKHOUSE_ADDR is required")
	}
	if cfg.Redis.Addr == "" {
		add("REDIS_ADDR is required")
	}
	if cfg.ControlPlane.InternalToken == "" {
		add("INTERNAL_API_TOKEN is required")
	}
	if len(problems) > 0 {
		return QueryConfig{}, fmt.Errorf("invalid query configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}

// AlerterConfig configures the alerting service: the rule/alert HTTP API plus the
// background evaluation loop. It reads rules from Postgres, metrics from
// ClickHouse, caches auth in Redis, and verifies admin-scoped keys against the
// control plane.
type AlerterConfig struct {
	Env                 Environment
	Log                 LogConfig
	OTel                OTelConfig
	HTTP                HTTPConfig
	DB                  DatabaseConfig
	ClickHouse          ClickHouseConfig
	Redis               RedisConfig
	ControlPlane        ControlPlaneClientConfig
	EvalInterval        time.Duration
	WebhookTimeout      time.Duration
	MaxExecutionSeconds int
}

func LoadAlerter() (AlerterConfig, error) {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	cfg := AlerterConfig{
		Env:  Environment(getStr("APP_ENV", string(EnvLocal))),
		Log:  loadLog(),
		OTel: loadOTel("prism-alerter"),
		HTTP: HTTPConfig{
			Host:            getStr("HTTP_HOST", "0.0.0.0"),
			Port:            getInt("HTTP_PORT", 8093),
			ReadTimeout:     getDur("HTTP_READ_TIMEOUT", 30*time.Second),
			WriteTimeout:    getDur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			ShutdownTimeout: getDur("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		DB: DatabaseConfig{
			URL:             getStr("DATABASE_URL", ""),
			MaxConns:        getInt32("DATABASE_MAX_CONNS", 5),
			MinConns:        getInt32("DATABASE_MIN_CONNS", 1),
			ConnMaxLifetime: getDur("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		ClickHouse: ClickHouseConfig{
			Addr:         getStr("CLICKHOUSE_ADDR", "localhost:9000"),
			Database:     getStr("CLICKHOUSE_DATABASE", "default"),
			Username:     getStr("CLICKHOUSE_USERNAME", "default"),
			Password:     getStr("CLICKHOUSE_PASSWORD", ""),
			DialTimeout:  getDur("CLICKHOUSE_DIAL_TIMEOUT", 10*time.Second),
			MaxOpenConns: getInt("CLICKHOUSE_MAX_OPEN_CONNS", 10),
		},
		Redis: RedisConfig{
			Addr:     getStr("REDIS_ADDR", "localhost:6379"),
			Password: getStr("REDIS_PASSWORD", ""),
			DB:       getInt("REDIS_DB", 0),
			PoolSize: getInt("REDIS_POOL_SIZE", 10),
		},
		ControlPlane: ControlPlaneClientConfig{
			VerifyURL:        getStr("CONTROL_PLANE_VERIFY_URL", "http://localhost:8080/internal/v1/keys/verify"),
			InternalToken:    getStr("INTERNAL_API_TOKEN", ""),
			Timeout:          getDur("CONTROL_PLANE_TIMEOUT", 3*time.Second),
			CacheTTL:         getDur("AUTH_CACHE_TTL", 60*time.Second),
			NegativeCacheTTL: getDur("AUTH_NEGATIVE_CACHE_TTL", 10*time.Second),
		},
		EvalInterval:        getDur("ALERTER_EVAL_INTERVAL", 15*time.Second),
		WebhookTimeout:      getDur("ALERTER_WEBHOOK_TIMEOUT", 5*time.Second),
		MaxExecutionSeconds: getInt("ALERTER_MAX_EXECUTION_SECONDS", 15),
	}

	if cfg.DB.URL == "" {
		add("DATABASE_URL is required")
	}
	if cfg.ClickHouse.Addr == "" {
		add("CLICKHOUSE_ADDR is required")
	}
	if cfg.Redis.Addr == "" {
		add("REDIS_ADDR is required")
	}
	if cfg.ControlPlane.InternalToken == "" {
		add("INTERNAL_API_TOKEN is required")
	}
	if len(problems) > 0 {
		return AlerterConfig{}, fmt.Errorf("invalid alerter configuration:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return cfg, nil
}
