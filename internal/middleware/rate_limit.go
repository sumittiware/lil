package middleware

import (
	"net/http"

	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/middleware/stdlib"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

func RateLimiter(rate limiter.Rate) func(http.Handler) http.Handler {
	store := memory.NewStore()
	instance := limiter.New(store, rate)
	middleware := stdlib.NewMiddleware(instance, stdlib.WithKeyGetter(func(r *http.Request) string {
		// Custom key, e.g., using user agent and IP
		return r.Header.Get("X-Forwarded-For") + ":" + r.UserAgent()
	}))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			middleware.Handler(next).ServeHTTP(w, r)
		})
	}
}
