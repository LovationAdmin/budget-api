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

// 1. Authentification (JWT Token) - avec cache
func (s *BridgeService) GetAccessToken(ctx context.Context) (string, error) {
	// Cache le token s'il est encore valide
	if s.AccessToken != "" && time.Now().Before(s.TokenExpiry) {
		return s.AccessToken, nil
	}

	payload := map[string]string{
		"client_id":     s.ClientID,
		"client_secret": s.ClientSecret,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.bridgeapi.io/v3/authenticate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2025-01-01")

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
	s.TokenExpiry = time.Now().Add(25 * time.Minute)

	return s.AccessToken, nil
}

// 2. Créer une Connect Session (API v3)
func (s *BridgeService) CreateConnectItem(ctx context.Context, accessToken, userEmail string) (string, error) {
	payload := map[string]interface{}{
		"user_email": userEmail,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2025-01-01")

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
		ID  string `json:"id"`
		URL string `json:"url"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("bridge create session decode failed: %w", err)
	}

	return result.URL, nil
}

// 3. Lister les Items (connexions bancaires)
type BridgeItem struct {
	ID          int64  `json:"id"`
	Status      string `json:"status"`
	StatusCode  string `json:"status_code_info"`
	BankID      int    `json:"bank_id"`
}

func (s *BridgeService) GetItems(ctx context.Context, accessToken string) ([]BridgeItem, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/items", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2025-01-01")

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

// 4. Récupérer les comptes bancaires
type BridgeAccount struct {
	ID               int64   `json:"id"`
	ItemID           int64   `json:"item_id"`
	Name             string  `json:"name"`
	Balance          float64 `json:"balance"`
	Currency         string  `json:"currency_code"`
	IBAN             string  `json:"iban"`
	Type             string  `json:"type"`
	Status           string  `json:"status"`
	LastRefreshedAt  string  `json:"last_refreshed_at"`
}

func (s *BridgeService) GetAccounts(ctx context.Context, accessToken string) ([]BridgeAccount, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2025-01-01")

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

// 5. Lister les banques disponibles
type BridgeBank struct {
	ID               int      `json:"id"`
	Name             string   `json:"name"`
	LogoURL          string   `json:"logo_url"`
	CountryCodes     []string `json:"country_codes"`
	Capabilities     []string `json:"capabilities"`
	PrimaryColor     string   `json:"primary_color"`
	SecondaryColor   string   `json:"secondary_color"`
}

func (s *BridgeService) GetBanks(ctx context.Context, accessToken string) ([]BridgeBank, error) {
	// Récupérer les banques françaises uniquement
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/banks?country_codes=FR", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2025-01-01")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bridge get banks request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bridge get banks failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeBank `json:"resources"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("bridge get banks decode failed: %w", err)
	}

	return result.Resources, nil
}

// 6. Rafraîchir les comptes
func (s *BridgeService) RefreshAccounts(ctx context.Context, accessToken string, itemID int64) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/aggregation/items/%d/refresh", s.BaseURL, itemID), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2025-01-01")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("bridge refresh accounts request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bridge refresh accounts failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}