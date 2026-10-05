package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestKeyByClientIP_UsesVisitorNotProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := map[string]struct {
		headers map[string]string
		want    string
	}{
		"forwarded":        {map[string]string{"X-Forwarded-For": "203.0.113.7, 10.24.51.59"}, "ip:203.0.113.7"},
		"cloudflare first": {map[string]string{"CF-Connecting-IP": "198.51.100.2", "X-Forwarded-For": "203.0.113.7"}, "ip:198.51.100.2"},
		"no proxy header":  {nil, "ip:192.0.2.1"},
		"garbage":          {map[string]string{"X-Forwarded-For": "not-an-ip"}, "ip:192.0.2.1"},
	}
	for name, tc := range cases {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", nil)
		c.Request.RemoteAddr = "192.0.2.1:1234"
		for k, v := range tc.headers {
			c.Request.Header.Set(k, v)
		}
		if got, _ := KeyByClientIP(c); got != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}
