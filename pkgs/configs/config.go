package configs

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DirectorStoryPoints   int
	DirectorImagePoints   int
	DirectorVideoPoints   int
	DirectorSampleBaseURL string

	EditorAIEnabled     bool
	GeminiAPIKey        string
	GeminiModel         string
	GeminiModelVersion  string
	WhisperPython       string
	WhisperRunner       string
	WhisperModelPath    string
	WhisperModelVersion string
	WhisperDevice       string
	WhisperComputeType  string
	WhisperLanguage     string
	EditorASRProvider   string
	TyphoonPython       string
	TyphoonRunner       string
	TyphoonModelPath    string
	TyphoonModelVersion string

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

	ArkImageAPIKey              string
	ArkImageBaseURL             string
	ArkImageModel               string
	ArkStoryAnalysisModel       string
	ArkStoryAnalysisAPIKey      string
	ArkStoryAnalysisBaseURL     string
	ArkReferenceAnalysisModel   string
	ArkReferenceAnalysisAPIKey  string
	ArkReferenceAnalysisBaseURL string
	ArkAPIKey                   string
	ArkBaseURL                  string
	ArkDefaultModel             string
	ArkRequestTimeout           time.Duration
	ArkPollIntervalSeconds      int

	StorageProvider    string
	StorageBucket      string
	StorageImageBucket string
	StorageRegion      string
	StorageAccessKey   string
	StorageSecretKey   string
	StorageCDNBaseURL  string

	// FFmpegFontFile is the font used for text overlays in storyboard exports.
	FFmpegFontFile string

	CORSAllowedOrigins string

	// PublicWebBaseURL is the frontend origin used to build share links (<base>/s/<token>).
	PublicWebBaseURL string
	// PublicRateLimitPerMinute caps unauthenticated requests per client IP.
	PublicRateLimitPerMinute int

	RateLimitJobsPerMinute   int
	MaxConcurrentJobsPerUser int

	// DevUserID is injected by DevAuth middleware when auth is disabled.
	// Set to any UUID in .env for local development.
	DevUserID string
}

func Load() *Config {
	return &Config{
		EditorAIEnabled:     strings.EqualFold(os.Getenv("EDITOR_AI_ENABLED"), "true"),
		GeminiAPIKey:        os.Getenv("GEMINI_API_KEY"),
		GeminiModel:         getEnv("GEMINI_MODEL", "gemini-3.5-flash-lite"),
		GeminiModelVersion:  os.Getenv("GEMINI_MODEL_VERSION"),
		WhisperPython:       os.Getenv("WHISPER_PYTHON"),
		WhisperRunner:       getEnv("WHISPER_RUNNER", "scripts/whisper/transcribe.py"),
		WhisperModelPath:    os.Getenv("WHISPER_MODEL_PATH"),
		WhisperModelVersion: os.Getenv("WHISPER_MODEL_VERSION"),
		WhisperDevice:       getEnv("WHISPER_DEVICE", "cpu"),
		WhisperComputeType:  getEnv("WHISPER_COMPUTE_TYPE", "int8"),
		WhisperLanguage:     os.Getenv("WHISPER_LANGUAGE"),
		EditorASRProvider:   strings.ToLower(strings.TrimSpace(getEnv("EDITOR_ASR_PROVIDER", "whisper"))),
		TyphoonPython:       os.Getenv("TYPHOON_PYTHON"),
		TyphoonRunner:       getEnv("TYPHOON_RUNNER", "scripts/typhoon/transcribe.py"),
		TyphoonModelPath:    os.Getenv("TYPHOON_MODEL_PATH"),
		TyphoonModelVersion: os.Getenv("TYPHOON_MODEL_VERSION"),
		Port:                getEnv("PORT", "8080"),
		Env:                 getEnv("ENV", "development"),

		DatabaseURL:          mustEnv("DATABASE_URL"),
		DatabaseMaxOpenConns: getEnvInt("DATABASE_MAX_OPEN_CONNS", 25),
		DatabaseMaxIdleConns: getEnvInt("DATABASE_MAX_IDLE_CONNS", 5),

		JWTSecret:      mustEnv("JWT_SECRET"),
		JWTExpiryHours: getEnvInt("JWT_EXPIRY_HOURS", 24),

		SupabaseURL:            os.Getenv("SUPABASE_URL"),
		SupabaseAnonKey:        os.Getenv("SUPABASE_ANON_KEY"),
		SupabaseServiceRoleKey: os.Getenv("SUPABASE_SERVICE_ROLE_KEY"),

		ArkImageAPIKey:              getEnv("ARK_IMAGE_API_KEY", os.Getenv("ARK_API_KEY")),
		ArkImageBaseURL:             getEnv("ARK_IMAGE_BASE_URL", getEnv("ARK_BASE_URL", "https://ark.ap-southeast.bytepluses.com/api/v3")),
		ArkImageModel:               getEnv("ARK_IMAGE_MODEL", "seedream-5-0-260128"),
		DirectorStoryPoints:         getEnvInt("DIRECTOR_STORY_POINTS", 4),
		DirectorImagePoints:         getEnvInt("DIRECTOR_IMAGE_POINTS", 3),
		DirectorVideoPoints:         getEnvInt("DIRECTOR_VIDEO_POINTS", 20),
		DirectorSampleBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("DIRECTOR_SAMPLE_BASE_URL")), "/"),
		ArkStoryAnalysisModel:       strings.TrimSpace(os.Getenv("ARK_STORY_ANALYSIS_MODEL")),
		ArkStoryAnalysisAPIKey:      getEnv("ARK_STORY_ANALYSIS_API_KEY", getEnv("ARK_REFERENCE_ANALYSIS_API_KEY", getEnv("ARK_IMAGE_API_KEY", os.Getenv("ARK_API_KEY")))),
		ArkStoryAnalysisBaseURL:     getEnv("ARK_STORY_ANALYSIS_BASE_URL", getEnv("ARK_REFERENCE_ANALYSIS_BASE_URL", getEnv("ARK_IMAGE_BASE_URL", getEnv("ARK_BASE_URL", "https://ark.ap-southeast.bytepluses.com/api/v3")))),
		ArkReferenceAnalysisModel:   strings.TrimSpace(os.Getenv("ARK_REFERENCE_ANALYSIS_MODEL")),
		ArkReferenceAnalysisAPIKey:  getEnv("ARK_REFERENCE_ANALYSIS_API_KEY", getEnv("ARK_IMAGE_API_KEY", os.Getenv("ARK_API_KEY"))),
		ArkReferenceAnalysisBaseURL: getEnv("ARK_REFERENCE_ANALYSIS_BASE_URL", getEnv("ARK_IMAGE_BASE_URL", getEnv("ARK_BASE_URL", "https://ark.ap-southeast.bytepluses.com/api/v3"))),
		ArkAPIKey:                   os.Getenv("ARK_API_KEY"),
		ArkBaseURL:                  getEnv("ARK_BASE_URL", "https://ark.ap-southeast.bytepluses.com"),
		ArkDefaultModel:             os.Getenv("ARK_DEFAULT_MODEL"),
		ArkRequestTimeout:           time.Duration(getEnvInt("ARK_REQUEST_TIMEOUT_SECONDS", 300)) * time.Second,
		ArkPollIntervalSeconds:      getEnvInt("ARK_POLL_INTERVAL_SECONDS", 5),

		StorageProvider:    os.Getenv("STORAGE_PROVIDER"),
		StorageBucket:      os.Getenv("STORAGE_BUCKET"),
		StorageImageBucket: getEnv("STORAGE_IMAGE_BUCKET", "violet-characters"),
		StorageRegion:      os.Getenv("STORAGE_REGION"),
		StorageAccessKey:   os.Getenv("STORAGE_ACCESS_KEY"),
		StorageSecretKey:   os.Getenv("STORAGE_SECRET_KEY"),
		StorageCDNBaseURL:  os.Getenv("STORAGE_CDN_BASE_URL"),

		FFmpegFontFile: getEnv("FFMPEG_FONT_FILE", "/usr/share/fonts/dejavu/DejaVuSans.ttf"),

		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:3000"),

		PublicWebBaseURL:         getEnv("PUBLIC_WEB_BASE_URL", "http://localhost:3000"),
		PublicRateLimitPerMinute: getEnvInt("PUBLIC_RATE_LIMIT_PER_MINUTE", 120),

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
