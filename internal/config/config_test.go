package config

import "testing"

func TestLoadDefaultsToMCPDisabled(t *testing.T) {
	t.Setenv("MCP_ENABLED", "")
	t.Setenv("MCP_PATH", "")
	t.Setenv("JWT_SECRET", "local-development-secret-change-me")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPEnabled {
		t.Fatal("MCP should be disabled by default")
	}
	if cfg.MCPPath != "/mcp" {
		t.Fatalf("MCP path = %q", cfg.MCPPath)
	}
}

func TestLoadRejectsInvalidMCPConfiguration(t *testing.T) {
	t.Setenv("MCP_ENABLED", "sometimes")
	t.Setenv("JWT_SECRET", "local-development-secret-change-me")

	if _, err := Load(); err == nil {
		t.Fatal("expected invalid MCP_ENABLED to fail")
	}
}
