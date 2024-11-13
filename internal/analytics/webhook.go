package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type WebhookConfig struct {
	Endpoint string
	Timeout  time.Duration
	Headers  map[string]string
}

type WebhookDispatcher struct {
	config WebhookConfig
	client *http.Client
	logger *slog.Logger
}

func NewWebhookDispatcher(config WebhookConfig, logger *slog.Logger) (*WebhookDispatcher, error) {
	if config.Endpoint == "" {
		return nil, fmt.Errorf("webhook endpoint is required")
	}
	if config.Timeout == 0 {
		return nil, fmt.Errorf("webhook timeout is required")
	}

	return &WebhookDispatcher{
		config: config,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		logger: logger,
	}, nil
}

func (w *WebhookDispatcher) Name() string {
	return "webhook"
}

func (w *WebhookDispatcher) Send(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.config.Endpoint, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set default Content-Type if not specified in headers
	if _, exists := w.config.Headers["Content-Type"]; !exists {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set custom headers
	for k, v := range w.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook request failed with status: %d", resp.StatusCode)
	}

	return nil
}

// noop
func (w *WebhookDispatcher) Close() error {
	return nil
}
