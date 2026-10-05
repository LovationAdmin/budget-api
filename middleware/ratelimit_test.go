package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// keyFor runs KeyByIP for a request on an engine configured like main.go.
func keyFor(t *testing.T, onRender bool, headers map[string]string) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if onRender {
		t.Setenv("RENDER", "true")
	} else {
		t.Setenv("RENDER", "")
	}
	t.Setenv("CLIENT_IP_HEADER", "")

	w := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(w)
	_ = engine.SetTrustedProxies(nil)
	ConfigureClientIP(engine)

	c.Request = httptest.NewRequest("POST", "/", nil)
	c.Request.RemoteAddr = "10.24.51.59:4321" // Render's internal proxy
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	key, _ := KeyByIP(c)
	return key
}

func TestKeyByIP_OnRenderUsesCloudflareVisitorIP(t *testing.T) {
	got := keyFor(t, true, map[string]string{
		"CF-Connecting-IP": "198.51.100.2",
		"X-Forwarded-For":  "1.2.3.4, 198.51.100.2", // leftmost entry forged by the caller
	})
	if got != "ip:198.51.100.2" {
		t.Fatalf("got %q, want the Cloudflare visitor IP", got)
	}
}

func TestKeyByIP_NeverTrustsForwardedFor(t *testing.T) {
	for _, onRender := range []bool{true, false} {
		got := keyFor(t, onRender, map[string]string{"X-Forwarded-For": "1.2.3.4"})
		if got != "ip:10.24.51.59" {
			t.Fatalf("onRender=%v: got %q, a forged X-Forwarded-For must be ignored", onRender, got)
		}
	}
}

func TestKeyByIP_OffRenderIgnoresEdgeHeader(t *testing.T) {
	// Without an edge in front, the header could be forged.
	got := keyFor(t, false, map[string]string{"CF-Connecting-IP": "198.51.100.2"})
	if got != "ip:10.24.51.59" {
		t.Fatalf("got %q, want the TCP peer address", got)
	}
}
