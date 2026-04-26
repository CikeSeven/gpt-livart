package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gpt-livart/internal/api"
)

type Config struct {
	HTTPAddr       string
	PostgresDSN    string
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOUseSSL    bool
	API            api.Config
}

func Load() Config {
	port := env("SERVER_PORT", env("LIVART_PORT", "8080"))
	return Config{
		HTTPAddr:       ":" + port,
		PostgresDSN:    postgresDSN(),
		MinIOEndpoint:  env("MINIO_ENDPOINT", ""),
		MinIOAccessKey: env("MINIO_ACCESS_KEY", env("MINIO_ROOT_USER", "")),
		MinIOSecretKey: env("MINIO_SECRET_KEY", env("MINIO_ROOT_PASSWORD", "")),
		MinIOBucket:    env("MINIO_BUCKET", "livart"),
		MinIOUseSSL:    envBool("MINIO_USE_SSL", false),
		API: api.Config{
			JWTSecret:            env("JWT_SECRET", "livart-dev-jwt-secret-change-me-at-least-32-bytes"),
			JWTTTLDays:           envInt("JWT_TTL_DAYS", 30),
			DefaultAPIBaseURL:    strings.TrimRight(env("LIVART_DEFAULT_API_BASE_URL", ""), "/"),
			DefaultAPIKey:        env("LIVART_DEFAULT_API_KEY", ""),
			DefaultImageModel:    env("LIVART_DEFAULT_IMAGE_MODEL", "gpt-image-2"),
			DefaultChatModel:     env("LIVART_DEFAULT_CHAT_MODEL", "gpt-5.5"),
			AllowedCORSOrigins:   splitCSV(env("CORS_ALLOWED_ORIGINS", "http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173")),
			MaxUploadBytes:       int64(envInt("MAX_UPLOAD_MB", 25)) << 20,
			ExportTTL:            time.Hour,
			RequestTimeout:       time.Duration(envInt("WAIT_TIMEOUT_SECONDS", 120)) * time.Second,
			LocalObjectStorePath: env("LOCAL_OBJECT_STORE_PATH", "./data/objects"),
			StaticDir:            env("STATIC_DIR", "./frontend/dist"),
		},
	}
}

func postgresDSN() string {
	if dsn := env("DATABASE_URL", ""); dsn != "" {
		return dsn
	}
	host := env("DB_HOST", "localhost")
	port := env("DB_PORT", "5432")
	name := env("DB_NAME", env("POSTGRES_DB", "app"))
	user := env("DB_USER", env("POSTGRES_USER", "postgres"))
	password := env("DB_PASSWORD", env("POSTGRES_PASSWORD", ""))
	sslMode := env("DB_SSLMODE", "disable")
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, name, sslMode)
}

func env(name, fallback string) string {
	if value, ok := os.LookupEnv(name); ok {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(env(name, ""))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(env(name, "")))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
