package store

import (
	"context"
	"time"

	"github.com/mr-karan/lil/internal/metrics"
)

// StartExpiryWorker starts a background goroutine that periodically checks and removes expired URLs
func (s *Store) StartExpiryWorker(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				if err := s.removeExpiredURLs(ctx); err != nil {
					s.logger.Error("failed to remove expired URLs", "error", err)
				}
			}
		}
	}()
	s.logger.Info("started URL expiry worker")
}

// removeExpiredURLs removes all expired URLs from both the database and cache
func (s *Store) removeExpiredURLs(ctx context.Context) error {
	// Query for expired URLs
	rows, err := s.db.QueryContext(ctx,
		`DELETE FROM urls
		 WHERE expires_at IS NOT NULL
		 AND expires_at <= datetime('now')
		 RETURNING short_code`)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Remove expired URLs from cache
	s.mu.Lock()
	for rows.Next() {
		var shortCode string
		if err := rows.Scan(&shortCode); err != nil {
			s.mu.Unlock()
			return err
		}
		delete(s.cache, shortCode)
	}
	// Update metrics
	metrics.URLsStoredGauge.Set(float64(len(s.cache)))
	s.mu.Unlock()

	if err := rows.Err(); err != nil {
		return err
	}

	return nil
}
