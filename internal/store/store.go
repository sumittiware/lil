package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	rand "math/rand/v2"
	"strings"
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

	// Write buffer components
	writeBuf    []models.URLData
	bufMu       sync.Mutex
	bufferSize  int
	flushTicker *time.Ticker
	done        chan struct{}
	flushChan   chan []models.URLData
	workerDone  chan struct{}
}

type Conf struct {
	DBPath              string
	MaxOpenConns        int
	MaxIdleConns        int
	ConnMaxLifetimeMins int
	ShortURLLength      int
	BufferSize          int // Number of URLs to buffer before flush
	FlushInterval       time.Duration
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
		bufferSize:  cfg.BufferSize,
		writeBuf:    make([]models.URLData, 0, cfg.BufferSize),
		flushTicker: time.NewTicker(cfg.FlushInterval),
		done:        make(chan struct{}),
		flushChan:   make(chan []models.URLData, 100), // Buffer channel for pending flushes
		workerDone:  make(chan struct{}),
	}

	// Start single flush worker
	go s.flushWorker()

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
	s.flushTicker.Stop()
	close(s.done)
	close(s.flushChan)
	<-s.workerDone // Wait for worker to finish
	return s.db.Close()
}

func (s *Store) flushWorker() {
	defer close(s.workerDone)

	for {
		select {
		case <-s.flushTicker.C:
			s.triggerFlush()
		case urls, ok := <-s.flushChan:
			if !ok {
				return
			}
			s.flushWithRetry(urls)
		case <-s.done:
			return
		}
	}
}

func (s *Store) triggerFlush() {
	s.bufMu.Lock()
	if len(s.writeBuf) == 0 {
		s.bufMu.Unlock()
		return
	}

	// Copy buffer and reset it
	urls := make([]models.URLData, len(s.writeBuf))
	copy(urls, s.writeBuf)
	s.writeBuf = s.writeBuf[:0]
	s.bufMu.Unlock()

	// Send to flush channel
	select {
	case s.flushChan <- urls:
	default:
		s.logger.Warn("flush channel full, dropping batch", "count", len(urls))
	}
}

func (s *Store) flushWithRetry(urls []models.URLData) {
	const maxRetries = 3
	const retryDelay = 100 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := s.doFlush(urls); err != nil {
			if attempt < maxRetries-1 {
				s.logger.Warn("flush failed, retrying",
					"error", err,
					"attempt", attempt+1,
					"count", len(urls))
				time.Sleep(retryDelay * time.Duration(attempt+1))
				continue
			}
			s.logger.Error("flush failed after retries",
				"error", err,
				"count", len(urls))
		}
		return
	}
}

func (s *Store) doFlush(urls []models.URLData) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Build a single INSERT statement with multiple VALUES clauses
	var sb strings.Builder
	sb.WriteString(`INSERT INTO urls (short_code, url, title, created_at, expires_at) VALUES `)

	vals := make([]interface{}, 0, len(urls)*5) // 5 fields per URL

	for i, urlData := range urls {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("(?,?,?,?,?)")

		vals = append(vals,
			urlData.ShortCode,
			urlData.URL,
			urlData.Title,
			urlData.CreatedAt,
			urlData.ExpiresAt,
		)
	}

	// Execute single batch insert
	if _, err := tx.Exec(sb.String(), vals...); err != nil {
		return fmt.Errorf("batch insert: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	s.logger.Info("flushed urls to database", "count", len(urls))
	return nil
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

	if expiry > 0 {
		t := createdAt.Add(expiry)
		urlData.ExpiresAt = &t
	}

	// Update cache immediately
	s.mu.Lock()
	s.cache[shortCode] = urlData
	metrics.URLsStoredGauge.Set(float64(len(s.cache)))
	s.mu.Unlock()

	// Add to write buffer
	s.bufMu.Lock()
	s.writeBuf = append(s.writeBuf, urlData)
	shouldFlush := len(s.writeBuf) >= s.bufferSize
	s.bufMu.Unlock()

	// Trigger flush if buffer is full
	if shouldFlush {
		s.triggerFlush()
	}

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

func (s *Store) CreateShortURLs(ctx context.Context, urls []models.URLData) []map[string]string {
	var results []map[string]string

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, urlData := range urls {
		var shortCode string
		if urlData.ShortCode != "" {
			_, exists := s.cache[urlData.ShortCode]
			if exists {
				results = append(results, map[string]string{
					"url":   urlData.URL,
					"error": "slug already exists",
				})
				continue
			}
			shortCode = urlData.ShortCode
		} else {
			shortCode = generateRandomString(s.shortURLLen)
			for {
				_, exists := s.cache[shortCode]
				if !exists {
					break
				}
				shortCode = generateRandomString(s.shortURLLen)
			}
		}

		createdAt := time.Now()
		urlData.ShortCode = shortCode
		urlData.CreatedAt = createdAt

		if urlData.ExpiresAt != nil && urlData.ExpiresAt.After(createdAt) {
			expiry := time.Until(*urlData.ExpiresAt)
			t := createdAt.Add(expiry)
			urlData.ExpiresAt = &t
		}

		s.cache[shortCode] = urlData
		s.writeBuf = append(s.writeBuf, urlData)

		results = append(results, map[string]string{
			"url":      urlData.URL,
			"shortUrl": shortCode,
		})
	}

	if len(s.writeBuf) >= s.bufferSize {
		s.triggerFlush()
	}

	return results
}

// generateRandomString creates a random string of specified length
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Int32N(int32(len(charset)))]
	}
	return string(b)
}
