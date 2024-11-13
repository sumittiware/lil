package middleware

import (
	"crypto/subtle"
	"net/http"
)

// BasicAuth middleware implements HTTP Basic Authentication
func BasicAuth(username, password string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get credentials from request
			user, pass, ok := r.BasicAuth()
			if !ok {
				unauthorized(w)
				return
			}

			// Constant time comparison to prevent timing attacks
			usernameMatch := subtle.ConstantTimeCompare([]byte(user), []byte(username)) == 1
			passwordMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1

			if !usernameMatch || !passwordMatch {
				unauthorized(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}
