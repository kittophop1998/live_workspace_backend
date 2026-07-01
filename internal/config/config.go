package config

import (
	"fmt"
	"os"
	"strconv"
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
	MCPEnabled      bool
	MCPPath         string
}

func Load() (Config, error) {
	mcpEnabled, err := strconv.ParseBool(env("MCP_ENABLED", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("MCP_ENABLED must be true or false")
	}
	cfg := Config{
		HTTPAddr:        env("HTTP_ADDR", ":8080"),
		MongoURI:        env("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase:   env("MONGO_DATABASE", "fark_noi"),
		WorkspaceID:     env("WORKSPACE_ID", "wsp_demo"),
		JWTSecret:       env("JWT_SECRET", "local-development-secret-change-me"),
		DevCollaborator: env("DEV_COLLABORATOR_ID", "col_demo"),
		AllowedOrigins:  splitCSV(env("CORS_ORIGINS", "http://localhost:3000")),
		MCPEnabled:      mcpEnabled,
		MCPPath:         env("MCP_PATH", "/mcp"),
	}
	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		return Config{}, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	if !strings.HasPrefix(cfg.MCPPath, "/") || cfg.MCPPath == "/" {
		return Config{}, fmt.Errorf("MCP_PATH must be an absolute non-root path")
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
