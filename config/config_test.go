package config

import "testing"

func TestLoadAndDefaults(t *testing.T) {
	t.Setenv("PORT", "1234")
	t.Setenv("HOST", "127.0.0.1")
	t.Setenv("OPENWEATHERMAP_API_KEY", "key123")
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := Load()

	if cfg.Port != "1234" {
		t.Fatalf("expected port 1234 got %s", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("expected host 127.0.0.1 got %s", cfg.Host)
	}
	if cfg.OpenWeatherMapAPIKey != "key123" {
		t.Fatalf("expected api key key123 got %s", cfg.OpenWeatherMapAPIKey)
	}
	if !cfg.IsProduction() {
		t.Fatalf("expected production environment")
	}
	if cfg.GetServerAddress() != "127.0.0.1:1234" {
		t.Fatalf("unexpected server address: %s", cfg.GetServerAddress())
	}
}

func TestValidateMissingAPIKey(t *testing.T) {
	t.Setenv("OPENWEATHERMAP_API_KEY", "")
	cfg := Load()
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected Validate to warn about missing API key")
	}
}
