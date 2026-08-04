package config

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// devJWTSecret is only used when JWT_SECRET is unset in the local environment.
const devJWTSecret = "dev-secret-change-me"

// Config holds all configuration for the application, sourced from environment variables.
type Config struct {
	App       AppConfig
	Database  DatabaseConfig
	JWT       JWTConfig
	CORS      CORSConfig
	GoldPrice GoldPriceConfig
}

type AppConfig struct {
	Env  string // local | staging | production
	Port string
}

type DatabaseConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	Name            string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

type JWTConfig struct {
	Secret string
	Expiry time.Duration
}

type CORSConfig struct {
	AllowedOrigins []string
}

// GoldPriceConfig drives the BE-404 background sync (cmd/api/main.go),
// not the HTTP handler — it never reads env vars itself.
type GoldPriceConfig struct {
	SyncInterval time.Duration
}

// DSN builds a PostgreSQL connection string from the database config.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

// MigrateDSN builds a golang-migrate pgx5:// URL from the database config.
// Used by cmd/migrate and by the e2e test harness (both need the same
// connection string shape golang-migrate expects, distinct from DSN()).
func (d DatabaseConfig) MigrateDSN() string {
	u := url.URL{
		Scheme: "pgx5",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%s", d.Host, d.Port),
		Path:   "/" + d.Name,
	}
	q := u.Query()
	q.Set("sslmode", d.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

// Load reads configuration from environment variables. A .env file is loaded
// first if present (local development convenience); real env vars always win.
func Load() (*Config, error) {
	_ = godotenv.Load()

	appEnv := getEnv("APP_ENV", "local")

	jwtSecret := getEnv("JWT_SECRET", "")
	if jwtSecret == "" {
		if appEnv != "local" {
			return nil, fmt.Errorf("JWT_SECRET is required outside the local environment")
		}
		jwtSecret = devJWTSecret
		log.Println("WARNING: JWT_SECRET not set, using insecure development default")
	}

	cfg := &Config{
		App: AppConfig{
			Env: appEnv,
			// Railway (and most PaaS) inject PORT and expect the app to
			// listen on it; APP_PORT stays the local-dev override.
			Port: getEnv("PORT", getEnv("APP_PORT", "8080")),
		},
		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnv("DB_PORT", "5432"),
			User:            getEnv("DB_USER", "postgres"),
			Password:        getEnv("DB_PASSWORD", "postgres"),
			Name:            getEnv("DB_NAME", "gold_track"),
			SSLMode:         getEnv("DB_SSLMODE", "disable"),
			MaxConns:        int32(getEnvInt("DB_MAX_CONNS", 10)),
			MinConns:        int32(getEnvInt("DB_MIN_CONNS", 2)),
			MaxConnLifetime: getEnvDuration("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: getEnvDuration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
		},
		JWT: JWTConfig{
			Secret: jwtSecret,
			Expiry: getEnvDuration("JWT_EXPIRY", 24*time.Hour),
		},
		CORS: CORSConfig{
			AllowedOrigins: getEnvList("CORS_ALLOWED_ORIGINS", []string{"*"}),
		},
		GoldPrice: GoldPriceConfig{
			SyncInterval: getEnvDuration("GOLD_PRICE_SYNC_INTERVAL", time.Hour),
		},
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

// getEnvList reads a comma-separated env var into a trimmed string slice.
func getEnvList(key string, fallback []string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
