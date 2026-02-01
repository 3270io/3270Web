package main

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOriginRefererCheckMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		method         string
		headers        map[string]string
		host           string
		expectedStatus int
	}{
		{
			name:           "Safe method GET allowed",
			method:         "GET",
			headers:        map[string]string{},
			host:           "example.com",
			expectedStatus: 200,
		},
		{
			name:           "Safe method HEAD allowed",
			method:         "HEAD",
			headers:        map[string]string{},
			host:           "example.com",
			expectedStatus: 200,
		},
		{
			name: "Valid Origin matches Host",
			method: "POST",
			headers: map[string]string{
				"Origin": "https://example.com",
			},
			host:           "example.com",
			expectedStatus: 200,
		},
		{
			name: "Valid Origin matches Host with port",
			method: "POST",
			headers: map[string]string{
				"Origin": "http://localhost:8080",
			},
			host:           "localhost:8080",
			expectedStatus: 200,
		},
		{
			name: "Invalid Origin mismatch",
			method: "POST",
			headers: map[string]string{
				"Origin": "https://evil.com",
			},
			host:           "example.com",
			expectedStatus: 403,
		},
		{
			name: "Missing Origin, Valid Referer matches Host",
			method: "POST",
			headers: map[string]string{
				"Referer": "https://example.com/page",
			},
			host:           "example.com",
			expectedStatus: 200,
		},
		{
			name: "Missing Origin, Invalid Referer mismatch",
			method: "POST",
			headers: map[string]string{
				"Referer": "https://evil.com/page",
			},
			host:           "example.com",
			expectedStatus: 403,
		},
		{
			name: "Missing Origin and Referer",
			method: "POST",
			headers: map[string]string{},
			host:           "example.com",
			expectedStatus: 403,
		},
		{
			name: "Case insensitive Host matching",
			method: "POST",
			headers: map[string]string{
				"Origin": "https://EXAMPLE.COM",
			},
			host:           "example.com",
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(OriginRefererCheckMiddleware())
			r.Any("/", func(c *gin.Context) {
				c.Status(200)
			})

			req := httptest.NewRequest(tt.method, "/", nil)
			req.Host = tt.host
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

func TestIsSafeMethod(t *testing.T) {
	safe := []string{"GET", "HEAD", "OPTIONS", "TRACE"}
	unsafe := []string{"POST", "PUT", "DELETE", "PATCH", "CONNECT"}

	for _, m := range safe {
		if !isSafeMethod(m) {
			t.Errorf("isSafeMethod(%q) = false, want true", m)
		}
	}
	for _, m := range unsafe {
		if isSafeMethod(m) {
			t.Errorf("isSafeMethod(%q) = true, want false", m)
		}
	}
}

func TestIsValidHost(t *testing.T) {
	tests := []struct {
		got      string
		expected string
		want     bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "EXAMPLE.COM", true},
		{"localhost:8080", "localhost:8080", true},
		{"example.com", "evil.com", false},
	}

	for _, tt := range tests {
		if got := isValidHost(tt.got, tt.expected); got != tt.want {
			t.Errorf("isValidHost(%q, %q) = %v, want %v", tt.got, tt.expected, got, tt.want)
		}
	}
}
