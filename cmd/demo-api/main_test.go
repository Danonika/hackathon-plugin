package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestWithOrigin(origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/routing", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestOriginAllowed(t *testing.T) {
	allowed := []string{
		"",
		"http://localhost",
		"http://localhost:8787",
		"http://127.0.0.1:8787",
		"https://localhost:443",
		"http://[::1]:8787",
	}
	for _, origin := range allowed {
		if !originAllowed(requestWithOrigin(origin)) {
			t.Fatalf("origin %q must be allowed", origin)
		}
	}

	denied := []string{
		"http://localhost.evil.com",
		"http://127.0.0.1.evil.com",
		"http://localhostevil.com",
		"https://127.0.0.1.evil.com",
		"null",
		"ftp://localhost",
		"http://localhost@evil.com",
		"http://[::2]:8787",
	}
	for _, origin := range denied {
		if originAllowed(requestWithOrigin(origin)) {
			t.Fatalf("origin %q must be denied", origin)
		}
	}
}
