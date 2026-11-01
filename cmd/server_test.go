package cmd

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDeploymentClientIP(t *testing.T) {
	// Exercise the configuration uploaded by deploy-linux.sh, including both Docker gateways.
	config, err := os.ReadFile("../.env")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(config), "\n") {
		if strings.HasPrefix(line, "GOFLY_TRUSTED_PROXIES=") {
			t.Setenv("GOFLY_TRUSTED_PROXIES", strings.TrimPrefix(line, "GOFLY_TRUSTED_PROXIES="))
		}
	}
	r := gin.New()
	if err := r.SetTrustedProxies(trustedProxies()); err != nil {
		t.Fatal(err)
	}
	r.GET("/", func(c *gin.Context) { c.String(200, c.ClientIP()) })
	for _, tc := range []struct{ peer, forwarded, want string }{
		{"172.20.0.1:1234", "198.51.100.42", "198.51.100.42"},
		{"172.21.0.1:1234", "198.51.100.42", "198.51.100.42"},
		{"198.51.100.10:1234", "203.0.113.99", "198.51.100.10"},
		{"172.21.0.2:1234", "203.0.113.99", "172.21.0.2"},
		{"172.21.0.1:1234", "203.0.113.99, 198.51.100.42", "198.51.100.42"},
		{"172.21.0.1:1234", "invalid", "172.21.0.1"},
	} {
		t.Run(tc.peer+"/"+tc.forwarded, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.peer
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			response := httptest.NewRecorder()
			r.ServeHTTP(response, req)
			if got := response.Body.String(); got != tc.want {
				t.Fatalf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
