package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

// Empty namespace so common env keys match all Go backends: POSTGRES_URL, REDIS_ADDR, REDIS_PASSWORD.
const namespace = ""

// Config aggregates runtime configuration for the projects service.
type Config struct {
	App       AppConfig
	HTTP      HTTPConfig
	Postgres  PostgresConfig
	Redis     RedisConfig
	Events    EventsConfig
	Telemetry TelemetryConfig
	Auth      AuthConfig
	Services  ServicesConfig
	QA        QAConfig
}

// ServicesConfig holds the base URLs of the services projects-api calls over S2S.
type ServicesConfig struct {
	// TreasuryURL is treasury-api, which owns project budgets and costs (in-cluster DNS in
	// production, see devops-k8s apps/projects-api/values.yaml).
	TreasuryURL string `envconfig:"TREASURY_SERVICE_URL" default:"https://booksapi.codevertexafrica.com"`
	// ERPURL is erp-api, read for timesheet hours per project (project utilisation).
	ERPURL string `envconfig:"ERP_SERVICE_URL" default:"http://erp-api.erp.svc.cluster.local:80"`
	// MarketflowURL is marketflow-api, the CRM, searched for a project's client contact.
	MarketflowURL string `envconfig:"MARKETFLOW_API_URL" default:"http://marketflow-api.marketflow.svc.cluster.local:4000"`
	// TicketingURL is ticketing-api, where a severe QA failure opens a ticket (S2S).
	TicketingURL string `envconfig:"TICKETING_SERVICE_URL" default:"http://ticketing-api.ticketing.svc.cluster.local:4009"`
	// SubscriptionsURL is subscriptions-api, the owner of the feature catalogue QA criteria point at.
	SubscriptionsURL string `envconfig:"SUBSCRIPTIONS_SERVICE_URL" default:"http://subscription-api.subscriptions.svc.cluster.local:4000"`
}

// QAConfig configures the QA and acceptance module.
type QAConfig struct {
	// HashKey keys the email hashes on invites and results and signs invitee access tokens. When
	// empty a key is derived from INTERNAL_SERVICE_KEY; set it explicitly in production so that
	// rotating the service key does not orphan every invite.
	HashKey string `envconfig:"QA_HASH_KEY"`
	// AccessTTL is how long an invitee's access token lasts.
	AccessTTL time.Duration `envconfig:"QA_ACCESS_TTL" default:"8h"`
	// CookieSecure marks the invitee access cookie Secure (and SameSite=None). Off only for local
	// development over plain HTTP.
	CookieSecure bool `envconfig:"QA_COOKIE_SECURE" default:"true"`
	// CatalogEnabled turns on reading the feature catalogue from subscriptions-api. Off until the
	// catalogue S2S read ships; criteria then keep feature codes unchecked.
	CatalogEnabled bool `envconfig:"QA_CATALOG_ENABLED" default:"false"`
	// UIBaseURL builds the invite link sent to testers (https://.../qa/{code}).
	UIBaseURL string `envconfig:"QA_UI_BASE_URL" default:"https://projects.codevertexafrica.com"`
	// FieldEncryptionKey seals signer email addresses at rest (qa_invites.email_enc), so close and
	// decision notices can reach them. From the projects-api-secrets secret; when empty, signer
	// addresses are not kept and those notices go without a signers list.
	FieldEncryptionKey string `envconfig:"FIELD_ENCRYPTION_KEY"`
}

type AppConfig struct {
	Name    string `envconfig:"APP_NAME" default:"projects-service"`
	Env     string `envconfig:"APP_ENV" default:"development"`
	Region  string `envconfig:"APP_REGION" default:"africa-east-1"`
	Version string `envconfig:"APP_VERSION" default:"0.1.0"`
}

type HTTPConfig struct {
	Host           string        `envconfig:"HTTP_HOST" default:"0.0.0.0"`
	Port           int           `envconfig:"HTTP_PORT" default:"4007"`
	ReadTimeout    time.Duration `envconfig:"HTTP_READ_TIMEOUT" default:"20s"`
	WriteTimeout   time.Duration `envconfig:"HTTP_WRITE_TIMEOUT" default:"20s"`
	IdleTimeout    time.Duration `envconfig:"HTTP_IDLE_TIMEOUT" default:"90s"`
	TLSCertFile    string        `envconfig:"TLS_CERT_FILE"`
	TLSKeyFile     string        `envconfig:"TLS_KEY_FILE"`
	AllowedOrigins []string      `envconfig:"HTTP_ALLOWED_ORIGINS" default:"https://accounts.codevertexafrica.com,https://ordering.codevertexafrica.com,https://pos.codevertexafrica.com"`
}

// PostgresConfig holds database configuration.
// Defaults are conservative to prevent connection exhaustion on shared PostgreSQL (max_connections=200).
type PostgresConfig struct {
	URL             string        `envconfig:"POSTGRES_URL" default:"postgres://postgres:postgres@localhost:5432/projects?sslmode=disable"`
	MigrateURL      string        `envconfig:"POSTGRES_MIGRATE_URL"` // Direct PostgreSQL URL bypassing PgBouncer; falls back to URL if empty
	MaxOpenConns    int           `envconfig:"POSTGRES_MAX_OPEN_CONNS" default:"6"`
	MaxIdleConns    int           `envconfig:"POSTGRES_MAX_IDLE_CONNS" default:"3"`
	ConnMaxLifetime time.Duration `envconfig:"POSTGRES_CONN_MAX_LIFETIME" default:"15m"`
	RunMigrations   bool          `envconfig:"POSTGRES_RUN_MIGRATIONS" default:"false"`
}

type RedisConfig struct {
	Addr        string        `envconfig:"REDIS_ADDR" default:"localhost:6380"`
	Username    string        `envconfig:"REDIS_USERNAME"`
	Password    string        `envconfig:"REDIS_PASSWORD"`
	DB          int           `envconfig:"REDIS_DB" default:"0"`
	TLSRequired bool          `envconfig:"REDIS_TLS_REQUIRED" default:"false"`
	DialTimeout time.Duration `envconfig:"REDIS_DIAL_TIMEOUT" default:"5s"`
}

type EventsConfig struct {
	Bus           string `envconfig:"EVENT_BUS" default:"nats"`
	NATSURL       string `envconfig:"EVENTS_NATS_URL" default:"nats://localhost:4222"`
	StreamName    string `envconfig:"NATS_STREAM" default:"projects"`
	DeliverGroup  string `envconfig:"NATS_DELIVER_GROUP" default:"projects-workers"`
	DeadLetterJet string `envconfig:"NATS_DLQ_STREAM" default:"projects-dlq"`
}

type TelemetryConfig struct {
	OTLPEndpoint string `envconfig:"OTLP_ENDPOINT"`
	MetricsURL   string `envconfig:"METRICS_ENDPOINT"`
	TracingURL   string `envconfig:"TRACING_ENDPOINT"`
}

type AuthConfig struct {
	// Auth Service SSO (JWT) integration
	ServiceURL          string        `envconfig:"AUTH_SERVICE_URL" default:"https://sso.codevertexafrica.com"`
	Issuer              string        `envconfig:"AUTH_ISSUER" default:"https://sso.codevertexafrica.com"`
	Audience            string        `envconfig:"AUTH_AUDIENCE" default:"codevertex"`
	JWKSUrl             string        `envconfig:"AUTH_JWKS_URL" default:"https://sso.codevertexafrica.com/api/v1/.well-known/jwks.json"`
	JWKSCacheTTL        time.Duration `envconfig:"AUTH_JWKS_CACHE_TTL" default:"3600s"`
	JWKSRefreshInterval time.Duration `envconfig:"AUTH_JWKS_REFRESH_INTERVAL" default:"300s"`
	EnableAPIKeyAuth    bool          `envconfig:"AUTH_ENABLE_API_KEY_AUTH" default:"true"`
	APIKey              string        `envconfig:"INTERNAL_SERVICE_KEY" default:""`
}

// Load gathers configuration from environment variables and optional .env files.
func Load() (*Config, error) {
	_ = godotenv.Load()

	var cfg Config
	if err := envconfig.Process(namespace, &cfg); err != nil {
		return nil, fmt.Errorf("config: failed to load environment variables: %w", err)
	}

	return &cfg, nil
}
