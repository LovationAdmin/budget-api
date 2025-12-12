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
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/authenticate", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2021-06-01")

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

// 2. Créer un "Connect Item" (= connexion bancaire)
func (s *BridgeService) CreateConnectItem(ctx context.Context, accessToken, prefillEmail string) (string, error) {
	payload := map[string]interface{}{
		"prefill_email": prefillEmail,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/connect/items/add", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2021-06-01")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge create item request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bridge create item failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		RedirectURL string `json:"redirect_url"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("bridge create item decode failed: %w", err)
	}

	return result.RedirectURL, nil
}

// 3. Récupérer les comptes bancaires d'un utilisateur
type BridgeAccount struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Balance  float64 `json:"balance"`
	Currency string  `json:"currency_code"`
	IBAN     string  `json:"iban"`
	Type     string  `json:"type"`
}

func (s *BridgeService) GetAccounts(ctx context.Context, accessToken string, userID string) ([]BridgeAccount, error) {
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

// 4. Lister les banques disponibles
type BridgeBank struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	LogoURL  string `json:"logo_url"`
	Country  string `json:"country_code"`
}

func (s *BridgeService) GetBanks(ctx context.Context, accessToken string) ([]BridgeBank, error) {
	// Récupérer les banques françaises uniquement
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/banks?country_code=FR", nil)
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

// 5. Rafraîchir les données d'un compte
func (s *BridgeService) RefreshAccount(ctx context.Context, accessToken string, accountID int64) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/accounts/%d/refresh", s.BaseURL, accountID), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Bridge-Version", "2021-06-01")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("bridge refresh account request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("bridge refresh account failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}