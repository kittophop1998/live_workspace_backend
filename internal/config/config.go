package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr        string
	MongoURI        string
	MongoDatabase   string
	WorkspaceID     string
	JWTSecret       string
	DevCollaborator string
	AllowedOrigins  []string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		MongoURI:        env("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:   env("MONGO_DATABASE", "fark_noi"),
		WorkspaceID:     env("WORKSPACE_ID", "wsp_demo"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		DevCollaborator: env("DEV_COLLABORATOR_ID", "col_demo"),
		AllowedOrigins:  splitCSV(env("CORS_ORIGINS", "http://localhost:3000")),
	}
	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
