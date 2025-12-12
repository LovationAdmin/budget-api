package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type BridgeService struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	Client       *http.Client
	AccessToken  string
	TokenExpiry  time.Time
}

func NewBridgeService() *BridgeService {
	return &BridgeService{
		ClientID:     os.Getenv("BRIDGE_CLIENT_ID"),
		ClientSecret: os.Getenv("BRIDGE_CLIENT_SECRET"),
		BaseURL:      "https://api.bridgeapi.io/v3",
		Client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// Helper to set common headers
func (s *BridgeService) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2025-01-15")
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// 1. Authentification (Get Access Token)
func (s *BridgeService) GetAccessToken(ctx context.Context) (string, error) {
	// Return cached token if valid
	if s.AccessToken != "" && time.Now().Before(s.TokenExpiry) {
		return s.AccessToken, nil
	}

	// v3 endpoint for token generation
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/authorization/token", nil)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bridge auth failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("bridge auth decode failed: %w", err)
	}

	s.AccessToken = result.AccessToken
	// Default to 1 hour expiry for safety
	s.TokenExpiry = time.Now().Add(1 * time.Hour)

	return s.AccessToken, nil
}

// 2. Create Connect Session
func (s *BridgeService) CreateConnectItem(ctx context.Context, accessToken, userEmail string) (string, error) {
	payload := map[string]interface{}{
		"user_email": userEmail,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))
	
	// Add Authorization header
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge create session request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", fmt.Errorf("bridge create session failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		URL string `json:"url"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("bridge create session decode failed: %w", err)
	}

	return result.URL, nil
}

// 3. Get Items (Connected Banks)
type BridgeItem struct {
	ID          int64  `json:"id"`
	Status      int    `json:"status"`
	ProviderID  int    `json:"provider_id"`
}

func (s *BridgeService) GetItems(ctx context.Context, accessToken string) ([]BridgeItem, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/items", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge get items request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bridge get items failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeItem `json:"resources"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("bridge get items decode failed: %w", err)
	}

	return result.Resources, nil
}

// 4. Get Accounts
type BridgeAccount struct {
	ID               int64   `json:"id"`
	ItemID           int64   `json:"item_id"`
	Name             string  `json:"name"`
	Balance          float64 `json:"balance"`
	Currency         string  `json:"currency_code"`
	IBAN             string  `json:"iban"`
	Type             string  `json:"type"`
	Status           string  `json:"status"`
	DataAccess       string  `json:"data_access"`
}

func (s *BridgeService) GetAccounts(ctx context.Context, accessToken string) ([]BridgeAccount, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge get accounts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bridge get accounts failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeAccount `json:"resources"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("bridge get accounts decode failed: %w", err)
	}

	return result.Resources, nil
}

// 5. Get Providers (Replaces Banks)
type BridgeProvider struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	CountryCode      string   `json:"country_code"`
	Images           struct {
		Logo string `json:"logo"`
	} `json:"images"`
}

func (s *BridgeService) GetBanks(ctx context.Context, accessToken string) ([]BridgeProvider, error) {
	// v3 endpoint is /providers
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/providers?country_code=FR", nil)
	
	// Providers needs auth too
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge get providers request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bridge get providers failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeProvider `json:"resources"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("bridge get providers decode failed: %w", err)
	}

	return result.Resources, nil
}

// 6. Refresh Accounts
func (s *BridgeService) RefreshAccounts(ctx context.Context, accessToken string, itemID int64) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/aggregation/items/%d/refresh", s.BaseURL, itemID), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("bridge refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bridge refresh failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}