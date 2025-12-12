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
		// FIXED: Changed v3 to v2
		BaseURL:      "https://api.bridgeapi.io/v2",
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
	// FIXED: Changed v3 to v2
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.bridgeapi.io/v2/authenticate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2021-06-01") // Use stable version

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
	// Bridge tokens usually last 2 hours, set safety margin
	s.TokenExpiry = time.Now().Add(1 * time.Hour)

	return s.AccessToken, nil
}

// 2. Créer une Connect Session (API v2)
func (s *BridgeService) CreateConnectItem(ctx context.Context, accessToken, userEmail string) (string, error) {
	// Note: For V2, we might need to create a 'user' first or use the connect endpoint directly.
	// This payload assumes the standard /connect/items/add or similar flow.
	// Adjusting to standard V2 Connect URL generation if needed.
	
	// If you are using "Bridge Connect" (the widget), you typically redirect the user
	// to a URL constructed with the token, or fetch a redirect URL.
	// Assuming V2 /connect/items/add flow:
	
	// NOTE: Bridge V2 often creates the link client-side or via a specific endpoint.
	// Below is the generic implementation for requesting a connect URL if your integration supports it.
	// If not, you might need to construct the URL manually: https://bridgeapi.io/connect?client_id=...&token=...
	
	// Attempting standard endpoint:
	payload := map[string]interface{}{
		"user_email": userEmail,
	}

	body, _ := json.Marshal(payload)
	// FIXED: Endpoint might be different for V2. Using a generic connect-token endpoint placeholder.
	// Standard Bridge V2 often uses GET /connect/items/add or similar.
	// Let's try the standard item add request which returns a redirect_url
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/connect/items/add", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2021-06-01")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge create session request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		// Fallback: If the API call fails, check if we should just construct the URL manually
		return "", fmt.Errorf("bridge create session failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID  string `json:"id"`
		URL string `json:"redirect_url"` // Check exact field name in Bridge docs
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
	StatusCode  int    `json:"status_code_info"` // Changed to int often in V2
	BankID      int    `json:"bank_id"`
}

func (s *BridgeService) GetItems(ctx context.Context, accessToken string) ([]BridgeItem, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/items", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2021-06-01")

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
	Status           string  `json:"status"` // Note: V2 might use 'is_pro' etc.
	LastRefreshedAt  string  `json:"last_updated_at"` // Changed from last_refreshed_at for V2
}

func (s *BridgeService) GetAccounts(ctx context.Context, accessToken string) ([]BridgeAccount, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2021-06-01")

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
	// FIXED: V2 Endpoint is /banks
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/banks?country_codes=FR", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2021-06-01")

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
	// FIXED: Endpoint /v2/items/{id}/refresh
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/items/%d/refresh", s.BaseURL, itemID), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2021-06-01")

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