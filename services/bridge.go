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
}

func NewBridgeService() *BridgeService {
	return &BridgeService{
		ClientID:     os.Getenv("BRIDGE_CLIENT_ID"),
		ClientSecret: os.Getenv("BRIDGE_CLIENT_SECRET"),
		BaseURL:      "https://api.bridgeapi.io/v3",
		Client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *BridgeService) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2025-01-15")
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// Internal Helper: Get Token for a specific user (Create if needed)
func (s *BridgeService) getOrCreateUserToken(ctx context.Context, userEmail string) (string, error) {
	// 1. Try to create the user to get their UUID
	// If they exist, Bridge returns 409 but usually includes the UUID or we just auth with email?
	// Bridge V3 docs say we auth with user_uuid. 
	// To keep it robust: We try to Create. If 409, we assume they exist and try to List to find UUID.
	
	// Step A: Create User
	createPayload := map[string]string{"email": userEmail, "external_user_id": userEmail}
	body, _ := json.Marshal(createPayload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/users", bytes.NewBuffer(body))
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("user creation failed: %w", err)
	}
	defer resp.Body.Close()

	var userUUID string

	if resp.StatusCode == 201 {
		// Created successfully
		var res struct { Uuid string `json:"uuid"` }
		if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
			userUUID = res.Uuid
		}
	} 
	
	// If creation failed (e.g. 409) or we didn't get UUID, try to Find User by Email
	if userUUID == "" {
		listReq, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/users?external_user_id="+userEmail, nil)
		s.setHeaders(listReq)
		listResp, err := s.Client.Do(listReq)
		if err == nil {
			defer listResp.Body.Close()
			var listRes struct { Resources []struct { Uuid string `json:"uuid"` } `json:"resources"` }
			if err := json.NewDecoder(listResp.Body).Decode(&listRes); err == nil && len(listRes.Resources) > 0 {
				userUUID = listRes.Resources[0].Uuid
			}
		}
	}

	if userUUID == "" {
		return "", fmt.Errorf("could not resolve bridge user_uuid for email %s", userEmail)
	}

	// Step B: Generate Token for this UUID
	authPayload := map[string]string{"user_uuid": userUUID}
	authBody, _ := json.Marshal(authPayload)
	authReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/authorization/token", bytes.NewBuffer(authBody))
	s.setHeaders(authReq)

	authResp, err := s.Client.Do(authReq)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer authResp.Body.Close()

	if authResp.StatusCode != 200 {
		return "", fmt.Errorf("token request failed status: %d", authResp.StatusCode)
	}

	var tokenRes struct { AccessToken string `json:"access_token"` }
	if err := json.NewDecoder(authResp.Body).Decode(&tokenRes); err != nil {
		return "", err
	}

	return tokenRes.AccessToken, nil
}

// 1. Create Connect Session
func (s *BridgeService) CreateConnectItem(ctx context.Context, userEmail string) (string, error) {
	// Authenticate User First
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"user_email": userEmail,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))
	
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// FIX: Read body once here
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", fmt.Errorf("bridge error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode failed: %w", err)
	}

	return result.URL, nil
}

// 2. Get Banks (Providers) - Public App Auth
type BridgeProvider struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	CountryCode string `json:"country_code"`
	Images      struct {
		Logo string `json:"logo"`
	} `json:"images"`
}

func (s *BridgeService) GetBanks(ctx context.Context) ([]BridgeProvider, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/providers?country_code=FR", nil)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeProvider `json:"resources"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 3. Get Items (User Auth)
type BridgeItem struct {
	ID          int64  `json:"id"`
	Status      int    `json:"status"`
	ProviderID  int    `json:"provider_id"`
}

func (s *BridgeService) GetItems(ctx context.Context, userEmail string) ([]BridgeItem, error) {
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/items", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeItem `json:"resources"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 4. Get Accounts (User Auth)
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

func (s *BridgeService) GetAccounts(ctx context.Context, userEmail string) ([]BridgeAccount, error) {
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/accounts", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeAccount `json:"resources"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 5. Refresh Accounts (User Auth)
func (s *BridgeService) RefreshAccounts(ctx context.Context, userEmail string, itemID int64) error {
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return err
	}

	req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/aggregation/items/%d/refresh", s.BaseURL, itemID), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	return nil
}