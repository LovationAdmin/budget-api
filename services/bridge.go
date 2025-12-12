package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type BridgeService struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	Client       *http.Client
}

func NewBridgeService() *BridgeService {
	// SECURITY: Trim spaces to prevent 401 errors from accidental copy-pasting
	return &BridgeService{
		ClientID:     strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_SECRET")),
		BaseURL:      "https://api.bridgeapi.io/v3",
		Client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *BridgeService) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	// FIXED: Use a valid Bridge-Version date (check Bridge dashboard for latest)
	req.Header.Set("Bridge-Version", "2024-08-15")
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// hashEmail creates a safe external_user_id from email
// Bridge external_user_id should be alphanumeric, no special chars
func hashEmail(email string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(email)))
	return hex.EncodeToString(hash[:])[:32] // First 32 chars of SHA256
}

// 1. Get or Create User & Generate Token
func (s *BridgeService) getOrCreateUserToken(ctx context.Context, userEmail string) (string, error) {
	// FIXED: Use a hashed version of email as external_user_id
	// Bridge expects alphanumeric IDs without special characters
	externalID := hashEmail(userEmail)
	var userUUID string

	// --- STEP A: Try to Create the User ---
	// Doc: "Create a user with an external user id"
	createPayload := map[string]string{
		"external_user_id": externalID,
	}
	body, _ := json.Marshal(createPayload)

	createReq, err := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/users", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	s.setHeaders(createReq)

	// Debug logging (remove in production)
	fmt.Printf("[Bridge] Creating user with external_id: %s\n", externalID)
	fmt.Printf("[Bridge] Request URL: %s\n", s.BaseURL+"/aggregation/users")
	fmt.Printf("[Bridge] Headers: Client-Id=%s..., Bridge-Version=%s\n", s.ClientID[:8], "2024-08-15")

	createResp, err := s.Client.Do(createReq)
	if err != nil {
		return "", fmt.Errorf("connection error creating user: %w", err)
	}
	defer createResp.Body.Close()

	respBody, _ := io.ReadAll(createResp.Body)
	fmt.Printf("[Bridge] Create user response: %d - %s\n", createResp.StatusCode, string(respBody))

	if createResp.StatusCode == 201 || createResp.StatusCode == 200 {
		// SUCCESS: User Created
		var res struct {
			Uuid string `json:"uuid"`
		}
		if err := json.Unmarshal(respBody, &res); err == nil && res.Uuid != "" {
			userUUID = res.Uuid
			fmt.Printf("[Bridge] User created with UUID: %s\n", userUUID)
		}
	} else if createResp.StatusCode == 409 {
		// CONFLICT: User already exists.
		// We must fetch them to get their UUID using external_user_id
		fmt.Printf("[Bridge] User exists (409), fetching by external_user_id...\n")

		listURL := fmt.Sprintf("%s/aggregation/users?external_user_id=%s", s.BaseURL, url.QueryEscape(externalID))
		listReq, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
		s.setHeaders(listReq)

		listResp, err := s.Client.Do(listReq)
		if err == nil {
			defer listResp.Body.Close()
			listBody, _ := io.ReadAll(listResp.Body)
			fmt.Printf("[Bridge] List users response: %d - %s\n", listResp.StatusCode, string(listBody))

			var listRes struct {
				Resources []struct {
					Uuid string `json:"uuid"`
				} `json:"resources"`
			}
			if err := json.Unmarshal(listBody, &listRes); err == nil && len(listRes.Resources) > 0 {
				userUUID = listRes.Resources[0].Uuid
				fmt.Printf("[Bridge] Found existing user UUID: %s\n", userUUID)
			}
		}
	} else {
		// ERROR: Something else went wrong (e.g., 400, 401)
		return "", fmt.Errorf("bridge user creation failed (%d): %s", createResp.StatusCode, string(respBody))
	}

	if userUUID == "" {
		return "", fmt.Errorf("could not resolve user_uuid for external_id: %s", externalID)
	}

	// --- STEP B: Generate Token ---
	// Doc: "Authenticate user with the uuid"
	authPayload := map[string]string{"user_uuid": userUUID}
	authBody, _ := json.Marshal(authPayload)

	authReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/authorization/token", bytes.NewBuffer(authBody))
	s.setHeaders(authReq)

	authResp, err := s.Client.Do(authReq)
	if err != nil {
		return "", fmt.Errorf("token request error: %w", err)
	}
	defer authResp.Body.Close()

	authRespBody, _ := io.ReadAll(authResp.Body)
	fmt.Printf("[Bridge] Token response: %d - %s\n", authResp.StatusCode, string(authRespBody))

	if authResp.StatusCode != 200 {
		return "", fmt.Errorf("bridge token failed (%d): %s", authResp.StatusCode, string(authRespBody))
	}

	var tokenRes struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(authRespBody, &tokenRes); err != nil {
		return "", fmt.Errorf("token decode error: %w", err)
	}

	return tokenRes.AccessToken, nil
}

// 2. Create Connect Session
func (s *BridgeService) CreateConnectItem(ctx context.Context, userEmail string) (string, error) {
	// 1. Get Token
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return "", fmt.Errorf("failed to get user token: %w", err)
	}

	// 2. Create Connect Session
	// FIXED: user_email is REQUIRED for long-lived connections
	payload := map[string]interface{}{
		"user_email": userEmail,
		// Optional: Add callback_url for redirection after sync
		// "callback_url": os.Getenv("FRONTEND_URL") + "/banking/callback",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))

	// Add Bearer Token for User Context
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("[Bridge] Connect session response: %d - %s\n", resp.StatusCode, string(respBody))

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", fmt.Errorf("bridge connect error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("connect decode failed: %w", err)
	}

	return result.URL, nil
}

// 3. Get Providers (Public App Auth - No Token Needed)
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
	s.setHeaders(req) // Client-ID/Secret only

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, fmt.Errorf("unauthorized: check BRIDGE_CLIENT_ID/BRIDGE_CLIENT_SECRET in environment")
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeProvider `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 4. Get Items (Needs User Token)
type BridgeItem struct {
	ID         int64 `json:"id"`
	Status     int   `json:"status"`
	ProviderID int   `json:"provider_id"`
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

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeItem `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 5. Get Accounts (Needs User Token)
type BridgeAccount struct {
	ID         int64   `json:"id"`
	ItemID     int64   `json:"item_id"`
	Name       string  `json:"name"`
	Balance    float64 `json:"balance"`
	Currency   string  `json:"currency_code"`
	IBAN       string  `json:"iban"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	DataAccess string  `json:"data_access"`
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

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Resources []BridgeAccount `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 6. Refresh Item
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
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}