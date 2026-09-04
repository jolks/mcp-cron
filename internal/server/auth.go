// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// requireBearerToken wraps next, rejecting any request that does not carry
// an "Authorization: Bearer <token>" header matching token. Both sides are
// hashed with SHA-256 before the constant-time comparison, so response
// timing is independent of the presented token's length as well as its
// contents (ConstantTimeCompare alone short-circuits on length).
//
// Deliberately not the go-sdk's auth.RequireBearerToken: that middleware
// requires TokenInfo with a non-zero Expiration (a static shared token has
// none) and omits the WWW-Authenticate header unless an OAuth resource
// metadata URL is configured.
func requireBearerToken(token string, next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fields := strings.Fields(r.Header.Get("Authorization"))
		ok := len(fields) == 2 && strings.EqualFold(fields[0], "bearer")
		if ok {
			presented := sha256.Sum256([]byte(fields[1]))
			ok = subtle.ConstantTimeCompare(presented[:], expected[:]) == 1
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
