package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// Server
	Port string
	Env  string // development, staging, production

	// Database
	DatabaseURL string

	// Firebase
	FirebaseProjectID string

	// Claude API
	ClaudeAPIKey  string
	ClaudeBaseURL string

	// Job Feed
	RapidAPIKey string

	// Cloud Storage
	StorageBucket string

	// Rate Limiting
	RateLimitRPS int

	// CORS
	AllowedOrigins []string
}

func Load() (*Config, error) {
	// Load .env file if it exists (development only)
	loadEnvFile(".env")

	cfg := &Config{
		Port:           getEnv("PORT", "8080"),
		Env:            getEnv("ENV", "development"),
		DatabaseURL:    getEnv("DATABASE_URL", ""),
		FirebaseProjectID: getEnv("FIREBASE_PROJECT_ID", ""),
		ClaudeAPIKey:   getEnv("CLAUDE_API_KEY", ""),
		ClaudeBaseURL:  getEnv("CLAUDE_BASE_URL", "https://api.anthropic.com"),
		RapidAPIKey:    getEnv("RAPIDAPI_KEY", ""),
		StorageBucket:  getEnv("STORAGE_BUCKET", ""),
		RateLimitRPS:   getEnvInt("RATE_LIMIT_RPS", 10),
		AllowedOrigins: []string{
			"http://localhost:5173",
			"https://hireiq.app",
		},
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

// loadEnvFile reads a .env file and sets environment variables.
// Silently skips if the file doesn't exist (production uses real env vars).
func loadEnvFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first = sign
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Don't overwrite existing env vars (real env takes precedence)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return fallback
}
