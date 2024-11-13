package metrics

import (
	"github.com/VictoriaMetrics/metrics"
)

var (
	// Counter for total number of redirects
	RedirectsTotal = metrics.NewCounter(`lil_redirects_total`)

	// Counter for total number of URLs shortened
	URLsShortenedTotal = metrics.NewCounter(`lil_urls_shortened_total`)

	// Counter for total number of URLs deleted
	URLsDeletedTotal = metrics.NewCounter(`lil_urls_deleted_total`)

	// Counter for failed redirects (404s, expired URLs)
	RedirectFailuresTotal = metrics.NewCounter(`lil_redirect_failures_total`)

	// Gauge for number of URLs in store
	URLsStoredGauge = metrics.NewGauge(`lil_urls_stored_total`, nil)
)
