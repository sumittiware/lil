package models

import "time"

type URLData struct {
	URL       string     `json:"url"`
	Title     string     `json:"title,omitempty"`
	ShortCode string     `json:"short_code"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
}
