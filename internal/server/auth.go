// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireBearerToken wraps next, rejecting any request that does not carry
// an "Authorization: Bearer <token>" header matching token. The comparison
// is constant-time so the token cannot be recovered via timing side-channels.
//
// Deliberately not the go-sdk's auth.RequireBearerToken: that middleware
// requires TokenInfo with a non-zero Expiration (a static shared token has
// none) and omits the WWW-Authenticate header unless an OAuth resource
// metadata URL is configured.
func requireBearerToken(token string, next http.Handler) http.Handler {
	expected := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := strings.Fields(r.Header.Get("Authorization"))
		if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") ||
			subtle.ConstantTimeCompare([]byte(fields[1]), expected) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
