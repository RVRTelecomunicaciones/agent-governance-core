// Package config provides application configuration loaded from environment variables.
//
// Environment variables:
//
//	PORT          HTTP server port                (default: 8080)
//	DB_HOST       PostgreSQL host                 (default: localhost)
//	DB_PORT       PostgreSQL port                 (default: 5432)
//	DB_USER       PostgreSQL user                 (default: postgres)
//	DB_PASSWORD   PostgreSQL password             (default: postgres)
//	DB_NAME       PostgreSQL database             (default: governance)
//	DB_SSLMODE    PostgreSQL SSL mode             (default: disable)
//	LOG_LEVEL     Log level (debug,info,warn,error) (default: info)
package config

import (
	"os"
	"strconv"

	"github.com/russellcxl/agent-governance-core/internal/infrastructure/database"
)

// Config holds the full application configuration.
type Config struct {
	ServerPort int
	DB         database.Config
	LogLevel   string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		ServerPort: envInt("PORT", 8080),
		DB: database.Config{
			Host:     env("DB_HOST", "localhost"),
			Port:     envInt("DB_PORT", 5432),
			User:     env("DB_USER", "postgres"),
			Password: env("DB_PASSWORD", "postgres"),
			Database: env("DB_NAME", "governance"),
			SSLMode:  env("DB_SSLMODE", "disable"),
		},
		LogLevel: env("LOG_LEVEL", "info"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
