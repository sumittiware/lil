package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"log/slog"
	rand "math/rand/v2"
	"sync"
	"time"

	"github.com/mr-karan/lil/internal/metrics"
	"github.com/mr-karan/lil/models"
	_ "modernc.org/sqlite"
)

//go:embed pragmas.sql
var pragmas string

var ErrNotExist = errors.New("the URL does not exist")

type Store struct {
	db          *sql.DB
	cache       map[string]models.URLData
	mu          sync.RWMutex
	logger      *slog.Logger
	shortURLLen int
}

type Conf struct {
	DBPath              string
	MaxOpenConns        int
	MaxIdleConns        int
	ConnMaxLifetimeMins int
	ShortURLLength      int
}

func New(cfg Conf, logger *slog.Logger) (*Store, error) {
	db, err := sql.Open("sqlite", cfg.DBPath)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeMins) * time.Minute)

	// Create tables if they don't exist
	if err := initDB(db); err != nil {
		return nil, err
	}

	s := &Store{
		db:          db,
		cache:       make(map[string]models.URLData),
		logger:      logger,
		shortURLLen: cfg.ShortURLLength,
	}

	// Load all existing URLs into cache
	if err := s.loadCache(); err != nil {
		return nil, err
	}

	// Initialize URLs stored gauge
	metrics.URLsStoredGauge.Set(float64(len(s.cache)))

	return s, nil
}

func initDB(db *sql.DB) error {
	// Create tables
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS urls (
			short_code TEXT PRIMARY KEY,
			url TEXT NOT NULL,
			title TEXT,
			created_at DATETIME NOT NULL,
			expires_at DATETIME
		)
	`); err != nil {
		return err
	}

	// Apply PRAGMA statements
	if _, err := db.Exec(pragmas); err != nil {
		return err
	}

	return nil
}

func (s *Store) loadCache() error {
	rows, err := s.db.Query(`SELECT short_code, url, title, created_at, expires_at FROM urls`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var urlData models.URLData
		var expiresAt sql.NullTime
		err := rows.Scan(&urlData.ShortCode, &urlData.URL, &urlData.Title, &urlData.CreatedAt, &expiresAt)
		if err != nil {
			return err
		}
		if expiresAt.Valid {
			urlData.ExpiresAt = &expiresAt.Time
		}
		s.cache[urlData.ShortCode] = urlData
	}
	return rows.Err()
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreateShortURL(ctx context.Context, url, title string, slug string, expiry time.Duration) (string, error) {
	var shortCode string
	if slug != "" {
		s.mu.RLock()
		_, exists := s.cache[slug]
		s.mu.RUnlock()
		if exists {
			return "", errors.New("slug already exists")
		}
		shortCode = slug
	} else {
		shortCode = generateRandomString(s.shortURLLen)
		for {
			s.mu.RLock()
			_, exists := s.cache[shortCode]
			s.mu.RUnlock()
			if !exists {
				break
			}
			shortCode = generateRandomString(6)
		}
	}

	createdAt := time.Now()
	urlData := models.URLData{
		URL:       url,
		Title:     title,
		ShortCode: shortCode,
		CreatedAt: createdAt,
	}

	var expiresAt *time.Time
	if expiry > 0 {
		t := createdAt.Add(expiry)
		expiresAt = &t
		urlData.ExpiresAt = expiresAt
	}

	// Store in database
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO urls (short_code, url, title, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		shortCode, url, title, createdAt, expiresAt)
	if err != nil {
		return "", err
	}

	// Update cache
	s.mu.Lock()
	s.cache[shortCode] = urlData
	metrics.URLsStoredGauge.Set(float64(len(s.cache)))
	s.mu.Unlock()

	return shortCode, nil
}

func (s *Store) GetRedirectData(ctx context.Context, shortCode string) (models.URLData, error) {
	s.mu.RLock()
	urlData, exists := s.cache[shortCode]
	s.mu.RUnlock()

	if !exists {
		return models.URLData{}, ErrNotExist
	}

	if urlData.ExpiresAt != nil && time.Now().After(*urlData.ExpiresAt) {
		// URL has expired, remove it
		s.mu.Lock()
		delete(s.cache, shortCode)
		metrics.URLsStoredGauge.Set(float64(len(s.cache)))
		s.mu.Unlock()
		_, err := s.db.ExecContext(ctx, `DELETE FROM urls WHERE short_code = ?`, shortCode)
		if err != nil {
			s.logger.Error("failed to delete expired url", "error", err)
		}
		return models.URLData{}, ErrNotExist
	}

	return urlData, nil
}

func (s *Store) DeleteURL(ctx context.Context, shortCode string) error {
	// Delete from database
	result, err := s.db.ExecContext(ctx, `DELETE FROM urls WHERE short_code = ?`, shortCode)
	if err != nil {
		return err
	}

	// Check if any row was affected
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return ErrNotExist
	}

	// Delete from cache
	s.mu.Lock()
	delete(s.cache, shortCode)
	metrics.URLsStoredGauge.Set(float64(len(s.cache)))
	s.mu.Unlock()

	return nil
}

func (s *Store) GetURLs(ctx context.Context, page, perPage int64) ([]models.URLData, int64, error) {
	offset := (page - 1) * perPage
	rows, err := s.db.QueryContext(ctx,
		`SELECT short_code, url, title, created_at, expires_at
		FROM urls
		WHERE expires_at IS NULL OR expires_at > datetime('now')
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		perPage, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var urls []models.URLData
	for rows.Next() {
		var urlData models.URLData
		var expiresAt sql.NullTime
		err := rows.Scan(&urlData.ShortCode, &urlData.URL, &urlData.Title, &urlData.CreatedAt, &expiresAt)
		if err != nil {
			return nil, 0, err
		}
		if expiresAt.Valid {
			urlData.ExpiresAt = &expiresAt.Time
		}
		urls = append(urls, urlData)
	}
	// Get total count
	var total int64
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM urls WHERE expires_at IS NULL OR expires_at > datetime('now')`).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	return urls, total, rows.Err()
}

// TODO: Make this configurable from the config.
// generateRandomString creates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Int32N(int32(len(charset)))]
	}
	return string(b)
}
