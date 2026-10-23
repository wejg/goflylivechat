package common

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppConfigRejectsUnsafeOrigins(t *testing.T) {
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	t.Setenv("GOFLY_APP_ID", "test")
	t.Setenv("GOFLY_APP_INTEGRATION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOFLY_ENV", "production")
	t.Setenv("GOFLY_APP_PUBLIC_ORIGIN", "https://chat.example")
	for _, origin := range []string{"http://example.com", "https://a/b", "https://user:pass@example.com", "https://a?secret=1", "https://a/#x"} {
		t.Setenv("GOFLY_APP_API_BASE_URL", origin)
		if ValidateAppChatConfig() == nil {
			t.Fatalf("accepted %s", origin)
		}
	}
	t.Setenv("GOFLY_APP_API_BASE_URL", "https://app.example")
	if err := ValidateAppChatConfig(); err != nil {
		t.Fatal(err)
	}
}
func TestAppCookieOriginDoesNotTrustForwardedHeaders(t *testing.T) {
	t.Setenv("GOFLY_ENV", "development")
	t.Setenv("GOFLY_APP_PUBLIC_ORIGIN", "")
	r := httptest.NewRequest("POST", "http://chat.example/visitor/session", nil)
	r.Header.Set("Origin", "https://chat.example")
	r.Header.Set("X-Forwarded-Proto", "https")
	if AppSameOrigin(r) {
		t.Fatal("trusted spoofed TLS")
	}
	r.TLS = &tls.ConnectionState{}
	if !AppSameOrigin(r) {
		t.Fatal("rejected actual TLS origin")
	}
	r.Header.Del("Origin")
	if AppSameOrigin(r) {
		t.Fatal("missing origin accepted")
	}
}
func TestAppSourceTLSContractAndRedirectRejection(t *testing.T) {
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	t.Setenv("GOFLY_APP_ID", "test")
	t.Setenv("GOFLY_APP_INTEGRATION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOFLY_ENV", "production")
	t.Setenv("GOFLY_APP_PUBLIC_ORIGIN", "https://chat.example")
	redirect := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Support-Key") != "0123456789abcdef0123456789abcdef" || r.URL.Path != "/api/internal/support/tickets/consume" {
			t.Error("invalid authenticated contract")
		}
		if redirect {
			http.Redirect(w, r, "https://other.example", 302)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"code": "ok", "data": map[string]interface{}{"app_id": "test", "user_id": "123"}})
	}))
	defer server.Close()
	t.Setenv("GOFLY_APP_API_BASE_URL", server.URL)
	transport := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = transport })
	var identity AppIdentity
	if err := AppSourceRequest(context.Background(), "/api/internal/support/tickets/consume", map[string]string{"ticket": "ticket"}, &identity); err != nil || identity.UserID != "123" {
		t.Fatalf("source identity=%+v err=%v", identity, err)
	}
	redirect = true
	if err := AppSourceRequest(context.Background(), "/api/internal/support/tickets/consume", map[string]string{"ticket": "ticket"}, &identity); err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestAppConfigRequiresStrongAgentSigningKey(t *testing.T) {
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "true")
	t.Setenv("GOFLY_APP_ID", "test")
	t.Setenv("GOFLY_APP_API_BASE_URL", "https://app.example")
	t.Setenv("GOFLY_APP_PUBLIC_ORIGIN", "https://chat.example")
	t.Setenv("GOFLY_APP_INTEGRATION_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOFLY_ENV", "production")
	for _, secret := range []string{"", "golivechat", strings.Repeat("a", 64), "replace-with-a-long-random-jwt-secret", "change-this-long-default-secret-123456", "default-jwt-signing-secret-0123456789"} {
		t.Setenv("GOFLY_JWT_SECRET", secret)
		if err := ValidateAppChatConfig(); err == nil || !strings.Contains(err.Error(), "GOFLY_JWT_SECRET") {
			t.Fatalf("unsafe signing key accepted: %q err=%v", secret, err)
		}
	}
	t.Setenv("GOFLY_JWT_SECRET", "fedcba9876543210fedcba9876543210")
	if err := ValidateAppChatConfig(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLY_APP_CHAT_ENABLED", "false")
	t.Setenv("GOFLY_JWT_SECRET", "")
	if err := ValidateAppChatConfig(); err != nil {
		t.Fatal("legacy disabled mode unexpectedly blocked", err)
	}
}
