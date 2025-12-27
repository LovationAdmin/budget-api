package models

import "time"

// ============================================================================
// USER MODEL
// ============================================================================

type User struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Avatar        string    `json:"avatar,omitempty"`
	Country       string    `json:"country,omitempty"`       // ✅ NEW
	PostalCode    string    `json:"postal_code,omitempty"`   // ✅ NEW
	PasswordHash  string    `json:"-"` // Never expose in JSON
	TOTPSecret    string    `json:"-"` // Never expose in JSON
	TOTPEnabled   bool      `json:"totp_enabled"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ============================================================================
// USER LOCATION
// ============================================================================

type UserLocation struct {
	Country    string `json:"country"`
	PostalCode string `json:"postal_code,omitempty"`
}

// ============================================================================
// AUTHENTICATION REQUESTS
// ============================================================================

type SignupRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	TOTPCode string `json:"totp_code,omitempty"`
}

type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// ============================================================================
// PASSWORD & 2FA
// ============================================================================

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required,min=6"`
}

type TOTPSetupResponse struct {
	Secret string `json:"secret"`
	QRCode string `json:"qr_code"`
}

type VerifyTOTPRequest struct {
	Code string `json:"code" binding:"required,len=6"`
}