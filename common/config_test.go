package common

import "testing"

func TestMysqlConfigEnvironmentOverridesFile(t *testing.T) {
	t.Setenv("GOFLY_DB_SERVER", "db")
	t.Setenv("GOFLY_DB_PORT", "3307")
	t.Setenv("GOFLY_DB_DATABASE", "chat")
	t.Setenv("GOFLY_DB_USERNAME", "user")
	t.Setenv("GOFLY_DB_PASSWORD", "secret")

	conf := GetMysqlConf()
	if conf.Server != "db" || conf.Port != "3307" || conf.Database != "chat" || conf.Username != "user" || conf.Password != "secret" {
		t.Fatalf("environment values were not applied: %+v", conf)
	}
}

func TestAllowedOrigin(t *testing.T) {
	t.Setenv("GOFLY_ALLOWED_ORIGINS", "https://example.com, http://localhost:3000")
	if !IsAllowedOrigin("https://example.com") {
		t.Fatal("configured origin should be allowed")
	}
	if IsAllowedOrigin("https://evil.example") {
		t.Fatal("unconfigured origin should be rejected")
	}
}
