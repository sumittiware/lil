package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mr-karan/lil/internal/analytics"
	"github.com/mr-karan/lil/internal/metrics"
	"github.com/mr-karan/lil/internal/store"
	"github.com/mr-karan/lil/models"
)

type shortenURLRequest struct {
	URL          string `json:"url"`
	Title        string `json:"title,omitempty"`
	Slug         string `json:"slug,omitempty"`
	ExpiryInSecs *int64 `json:"expiry_in_secs,omitempty"`
}

// httpResp represents the structure of the JSON response envelope
type httpResp struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// sendResponse sends a JSON envelope to the HTTP response.
func (app *App) sendResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out, err := json.Marshal(httpResp{Status: "success", Data: data})
	if err != nil {
		app.sendErrorResponse(w, "Internal Server Error.", http.StatusInternalServerError, nil)
		return
	}

	w.Write(out)
}

// sendErrorResponse sends a JSON error envelope to the HTTP response.
func (app *App) sendErrorResponse(w http.ResponseWriter, message string, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)

	resp := httpResp{Status: "error",
		Message: message,
		Data:    data}
	out, _ := json.Marshal(resp)
	w.Write(out)
}

func (app *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	app.sendResponse(w, "Welcome to lil")
}

func (app *App) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second*2)
	defer cancel()

	if err := app.store.Ping(ctx); err != nil {
		app.logger.Error("Health check failed", "error", err)
		app.sendErrorResponse(w, "Service Unhealthy", http.StatusServiceUnavailable, nil)
		return
	}
	app.sendResponse(w, "healthy")
}

func (app *App) handleShortenURL(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req shortenURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		app.logger.Error("Invalid request body", "error", err)
		app.sendErrorResponse(w, "Invalid request body", http.StatusBadRequest, nil)
		return
	}

	// Basic validation
	if req.URL == "" {
		app.sendErrorResponse(w, "URL is required", http.StatusBadRequest, nil)
		return
	}

	// Calculate expiry time if provided
	var expiry time.Duration
	if req.ExpiryInSecs != nil && *req.ExpiryInSecs > 0 {
		expiry = time.Duration(*req.ExpiryInSecs) * time.Second
	}

	// Call store method to create short URL
	shortCode, err := app.store.CreateShortURL(context.TODO(), req.URL, req.Title, req.Slug, expiry)
	if err != nil {
		app.logger.Error("Failed to create short URL", "error", err, "url", req.URL)
		metrics.URLsShortenedTotal.Inc()
		app.sendErrorResponse(w, "Failed to create short URL", http.StatusInternalServerError, nil)
		return
	}

	// Return the shortened URL with public base URL
	app.sendResponse(w, map[string]interface{}{
		"short_code": shortCode,
		"public_url": ko.String("app.public_url"),
	})
}

func (app *App) handleDeleteURL(w http.ResponseWriter, r *http.Request) {
	// Extract shortCode from path
	shortCode := r.PathValue("shortCode")
	if shortCode == "" {
		app.sendErrorResponse(w, "Invalid short code", http.StatusBadRequest, nil)
		return
	}

	// Delete URL from store
	if err := app.store.DeleteURL(context.TODO(), shortCode); err != nil {
		if err == store.ErrNotExist {
			metrics.URLsDeletedTotal.Inc()
			app.sendErrorResponse(w, "URL not found", http.StatusNotFound, nil)
			return
		}
		app.logger.Error("Failed to delete URL", "error", err, "shortCode", shortCode)
		app.sendErrorResponse(w, "Internal server error", http.StatusInternalServerError, nil)
		return
	}

	// Return success with no content
	w.WriteHeader(http.StatusNoContent)
}

func (app *App) handleGetURLs(w http.ResponseWriter, r *http.Request) {
	// Get pagination parameters from query string
	page := r.URL.Query().Get("page")
	perPage := r.URL.Query().Get("per_page")

	// Convert to int64 with defaults
	pageNum := int64(1)
	if page != "" {
		if p, err := strconv.ParseInt(page, 10, 64); err == nil {
			pageNum = p
		}
	}

	perPageNum := int64(10)
	if perPage != "" {
		if pp, err := strconv.ParseInt(perPage, 10, 64); err == nil {
			perPageNum = pp
		}
	}

	// Fetch URLs from store
	urls, total, err := app.store.GetURLs(context.TODO(), pageNum, perPageNum)
	if err != nil {
		app.logger.Error("Failed to fetch URLs", "error", err)
		app.sendErrorResponse(w, "Failed to fetch URLs", http.StatusInternalServerError, nil)
		return
	}

	// Return the URLs
	app.sendResponse(w, map[string]interface{}{
		"urls":     urls,
		"page":     pageNum,
		"per_page": perPageNum,
		"count":    total,
	})
}

func (app *App) handleRedirect(w http.ResponseWriter, r *http.Request) {
	// Extract shortCode from path
	shortCode := r.PathValue("shortCode")
	if shortCode == "" {
		app.sendErrorResponse(w, "Invalid short code", http.StatusBadRequest, nil)
		return
	}

	// Get URL data from store
	urlData, err := app.store.GetRedirectData(context.TODO(), shortCode)
	if err != nil {
		if err == store.ErrNotExist {
			metrics.RedirectFailuresTotal.Inc()
			app.sendErrorResponse(w, "URL not found", http.StatusNotFound, nil)
			return
		}
		app.logger.Error("Failed to get URL data", "error", err, "shortCode", shortCode)
		app.sendErrorResponse(w, "Internal server error", http.StatusInternalServerError, nil)
		return
	}

	metrics.RedirectsTotal.Inc()
	if app.analytics != nil {
		app.analytics.Track(analytics.Event{
			Name:       "pageview",
			Domain:     r.Host,
			URL:        fmt.Sprintf("%s/%s", ko.String("app.public_url"), shortCode),
			Referrer:   r.Header.Get("Referer"),
			UserAgent:  r.UserAgent(),
			RemoteAddr: r.RemoteAddr,
			Timestamp:  time.Now().UTC().Format(time.RFC3339),
			ShortCode:  shortCode,
			TargetURL:  urlData.URL,
		})
	}

	// Ensure browsers don't cache the redirect response to prevent stale redirects
	// if the target URL is updated or the short link expires
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")

	w.Header().Set("Location", urlData.URL)
	w.WriteHeader(http.StatusFound)
}

func (app *App) handleBulkUpload(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20) // 10MB limit
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Unable to get file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		http.Error(w, "Unable to read CSV file", http.StatusInternalServerError)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]map[string]string, 0, len(records)-1) // Adjust initial capacity to skip the first record
	batchSize := 10                                         // Number of URLs to process in each batch
	batch := make([]models.URLData, 0, batchSize)

	processBatch := func(batch []models.URLData) {
		defer wg.Done()
		shortenedURLs := app.store.CreateShortURLs(context.TODO(), batch)
		mu.Lock()
		results = append(results, shortenedURLs...)
		mu.Unlock()
	}

	// Start loop from index 1 to skip the first entry
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) == 0 {
			continue
		}

		longURL := record[0]
		title := record[1]
		slug := record[2]
		expiry := record[3]

		var expiresAt *time.Time
		if expiry != "" {
			if expirySeconds, err := strconv.ParseInt(expiry, 10, 64); err == nil {
				expiration := time.Now().Add(time.Duration(expirySeconds) * time.Second)
				expiresAt = &expiration
			}
		}

		urlData := models.URLData{
			URL:       longURL,
			Title:     title,
			ShortCode: slug,
			CreatedAt: time.Now(),
			ExpiresAt: expiresAt,
		}

		batch = append(batch, urlData)

		if len(batch) == batchSize {
			wg.Add(1)
			go processBatch(batch)
			batch = make([]models.URLData, 0, batchSize)
		}
	}

	// Process any remaining records in the final batch
	if len(batch) > 0 {
		wg.Add(1)
		go processBatch(batch)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
	}
}
