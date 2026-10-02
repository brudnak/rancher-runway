package server_test

import (
	"net/http/httptest"
	"testing"

	"github.com/brudnak/ha-rancher-rke2/internal/server"
)

func TestRequestFromLoopback(t *testing.T) {
	for _, tt := range []struct {
		address string
		want    bool
	}{
		{"127.0.0.1:8080", true}, {"[::1]:8080", true}, {"::1", true},
		{"192.0.2.1:8080", false}, {"localhost:8080", false}, {"", false},
	} {
		t.Run(tt.address, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://localhost:8080", nil)
			req.RemoteAddr = tt.address
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			if got := server.RequestFromLoopback(req); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBrowserOriginCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name, origin, referer string
		want                  bool
	}{
		{"origin", "http://LOCALHOST:8080", "", true},
		{"referer fallback", "", "http://localhost:8080/cluster", true},
		{"missing headers", "", "", false},
		{"different port", "http://localhost:9090", "", false},
		{"different host", "http://example.com:8080", "", false},
		{"malformed", "://localhost:8080", "", false},
		{"host suffix", "http://localhost:8080.example.com", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://localhost:8080", nil)
			req.Header.Set("Origin", tt.origin)
			req.Header.Set("Referer", tt.referer)
			if got := server.SameOriginBrowserRequest(req); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
