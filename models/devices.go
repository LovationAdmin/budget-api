// models/devices.go
package models

import "time"

type UserDevice struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	ExpoPushToken string    `json:"expo_push_token"`
	Platform      string    `json:"platform"`
	AppVersion    string    `json:"app_version,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

type MagicLinkToken struct {
	Token     string     `json:"-"` // SHA-256 hash; raw is only known to the user
	UserID    string     `json:"user_id"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	IPAddress string     `json:"ip_address,omitempty"`
	UserAgent string     `json:"user_agent,omitempty"`
}
