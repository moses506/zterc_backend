package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	JWTTTL          time.Duration
	OTPTTL          time.Duration
	AllowedOrigins  []string
	UploadDir       string
	MaxUploadSizeMB int64
}

func Load() Config {
	return Config{
		Port:            getEnv("PORT", "8080"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://zetrc:zetrc@localhost:5432/zetrc?sslmode=disable"),
		JWTSecret:       getEnv("JWT_SECRET", "dev-secret-change-me"),
		JWTTTL:          getEnvDuration("JWT_TTL_HOURS", 24) * time.Hour,
		OTPTTL:          getEnvDuration("OTP_TTL_MINUTES", 10) * time.Minute,
		AllowedOrigins:  getEnvList("ALLOWED_ORIGINS", []string{"*"}),
		UploadDir:       getEnv("UPLOAD_DIR", "./uploads"),
		MaxUploadSizeMB: getEnvInt64("MAX_UPLOAD_SIZE_MB", 50),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback int64) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Duration(n)
		}
	}
	return time.Duration(fallback)
}

func getEnvInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvList(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
