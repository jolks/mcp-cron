// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireBearerToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := requireBearerToken("s3cret", next)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong", http.StatusUnauthorized},
		{"wrong scheme", "Basic s3cret", http.StatusUnauthorized},
		{"token only, no scheme", "s3cret", http.StatusUnauthorized},
		{"correct token", "Bearer s3cret", http.StatusOK},
		{"lowercase scheme", "bearer s3cret", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("expected WWW-Authenticate: Bearer header on 401, got %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
