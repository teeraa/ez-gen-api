package configs

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port string
	Env  string

	DatabaseURL          string
	DatabaseMaxOpenConns int
	DatabaseMaxIdleConns int

	JWTSecret      string
	JWTExpiryHours int

	// Supabase project credentials
	SupabaseURL            string
	SupabaseAnonKey        string
	SupabaseServiceRoleKey string

	ArkAPIKey              string
	ArkBaseURL             string
	ArkDefaultModel        string
	ArkRequestTimeout      time.Duration
	ArkPollIntervalSeconds int

	StorageProvider   string
	StorageBucket     string
	StorageRegion     string
	StorageAccessKey  string
	StorageSecretKey  string
	StorageCDNBaseURL string

	CORSAllowedOrigins string

	RateLimitJobsPerMinute   int
	MaxConcurrentJobsPerUser int

	// DevUserID is injected by DevAuth middleware when auth is disabled.
	// Set to any UUID in .env for local development.
	DevUserID string
}

func Load() *Config {
	return &Config{
		Port: getEnv("PORT", "8080"),
		Env:  getEnv("ENV", "development"),

		DatabaseURL:          mustEnv("DATABASE_URL"),
		DatabaseMaxOpenConns: getEnvInt("DATABASE_MAX_OPEN_CONNS", 25),
		DatabaseMaxIdleConns: getEnvInt("DATABASE_MAX_IDLE_CONNS", 5),

		JWTSecret:      mustEnv("JWT_SECRET"),
		JWTExpiryHours: getEnvInt("JWT_EXPIRY_HOURS", 24),

		SupabaseURL:            os.Getenv("SUPABASE_URL"),
		SupabaseAnonKey:        os.Getenv("SUPABASE_ANON_KEY"),
		SupabaseServiceRoleKey: os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),

		ArkAPIKey:              os.Getenv("ARK_API_KEY"),
		ArkBaseURL:             getEnv("ARK_BASE_URL", "https://ark.ap-southeast.bytepluses.com"),
		ArkDefaultModel:        os.Getenv("ARK_DEFAULT_MODEL"),
		ArkRequestTimeout:      time.Duration(getEnvInt("ARK_REQUEST_TIMEOUT_SECONDS", 300)) * time.Second,
		ArkPollIntervalSeconds: getEnvInt("ARK_POLL_INTERVAL_SECONDS", 5),

		StorageProvider:   os.Getenv("STORAGE_PROVIDER"),
		StorageBucket:     os.Getenv("STORAGE_BUCKET"),
		StorageRegion:     os.Getenv("STORAGE_REGION"),
		StorageAccessKey:  os.Getenv("STORAGE_ACCESS_KEY"),
		StorageSecretKey:  os.Getenv("STORAGE_SECRET_KEY"),
		StorageCDNBaseURL: os.Getenv("STORAGE_CDN_BASE_URL"),

		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),

		RateLimitJobsPerMinute:   getEnvInt("RATE_LIMIT_JOBS_PER_MINUTE", 10),
		MaxConcurrentJobsPerUser: getEnvInt("MAX_CONCURRENT_JOBS_PER_USER", 3),

		DevUserID: getEnv("DEV_USER_ID", "00000000-0000-0000-0000-000000000001"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		return ""
	}
	return v
}
