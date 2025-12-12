package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	// Security: Trim spaces to prevent 401 errors
	return &BridgeService{
		ClientID:     strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_SECRET")),
		BaseURL:      "https://api.bridgeapi.io/v3", // V3 Base URL
		Client:       &http.Client{Timeout: 30 * time.Second},
	}
}

// hashEmail creates a safe external_user_id from an email
func hashEmail(email string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(hash[:])
}

func (s *BridgeService) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	// FIX: Use the V3 version (2025) to match the /v3/ endpoints
	req.Header.Set("Bridge-Version", "2025-01-15") 
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// 1. Get or Create User & Generate Token
func (s *BridgeService) getOrCreateUserToken(ctx context.Context, userEmail string) (string, error) {
	// We use the hashed email as the unique external_user_id
	externalID := hashEmail(userEmail)
	var userUUID string

	// --- STEP A: Check if user exists (List Users) ---
	listURL := fmt.Sprintf("%s/aggregation/users?external_user_id=%s", s.BaseURL, externalID)
	listReq, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	s.setHeaders(listReq)

	log.Printf("[Bridge] Checking existence for external_id: %s", externalID)

	listResp, err := s.Client.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("connection error checking user: %w", err)
	}
	defer listResp.Body.Close()

	if listResp.StatusCode == 200 {
		var listRes struct {
			Resources []struct {
				Uuid string `json:"uuid"`
			} `json:"resources"`
		}
		if err := json.NewDecoder(listResp.Body).Decode(&listRes); err == nil && len(listRes.Resources) > 0 {
			userUUID = listRes.Resources[0].Uuid
			log.Printf("[Bridge] Found existing user UUID: %s", userUUID)
		}
	} else if listResp.StatusCode == 401 {
		return "", fmt.Errorf("bridge 401 unauthorized: check api keys")
	}

	// --- STEP B: Create User if not found ---
	if userUUID == "" {
		log.Printf("[Bridge] Creating new user for external_id: %s", externalID)
		
		// V3: Only send external_user_id (no email)
		createPayload := map[string]string{
			"external_user_id": externalID,
		}
		body, _ := json.Marshal(createPayload)
		createReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/users", bytes.NewBuffer(body))
		s.setHeaders(createReq)

		createResp, err := s.Client.Do(createReq)
		if err != nil {
			return "", fmt.Errorf("user creation request failed: %w", err)
		}
		defer createResp.Body.Close()

		if createResp.StatusCode == 201 {
			var createRes struct {
				Uuid string `json:"uuid"`
			}
			if err := json.NewDecoder(createResp.Body).Decode(&createRes); err == nil {
				userUUID = createRes.Uuid
			}
		} else {
			// Read error body for debugging
			b, _ := io.ReadAll(createResp.Body)
			log.Printf("[Bridge Error] Create User Failed: %s", string(b))
			
			if createResp.StatusCode == 409 {
				return "", fmt.Errorf("user conflict 409 but failed to list previously")
			}
			return "", fmt.Errorf("bridge user creation failed (%d): %s", createResp.StatusCode, string(b))
		}
	}

	if userUUID == "" {
		return "", fmt.Errorf("could not resolve user_uuid")
	}

	// --- STEP C: Generate Token ---
	authPayload := map[string]string{"user_uuid": userUUID}
	authBody, _ := json.Marshal(authPayload)
	
	authReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/authorization/token", bytes.NewBuffer(authBody))
	s.setHeaders(authReq)

	authResp, err := s.Client.Do(authReq)
	if err != nil {
		return "", fmt.Errorf("token request error: %w", err)
	}
	defer authResp.Body.Close()

	if authResp.StatusCode != 200 {
		b, _ := io.ReadAll(authResp.Body)
		log.Printf("[Bridge Error] Token Failed: %s", string(b))
		return "", fmt.Errorf("token error (%d): %s", authResp.StatusCode, string(b))
	}

	var tokenRes struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&tokenRes); err != nil {
		return "", fmt.Errorf("token decode error: %w", err)
	}

	return tokenRes.AccessToken, nil
}

// 2. Create Connect Session
func (s *BridgeService) CreateConnectItem(ctx context.Context, userEmail string) (string, error) {
	// 1. Get Token
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return "", err
	}

	// 2. Create Link
	// V3: We pass the real user_email here for the Connect UI
	payload := map[string]interface{}{
		"user_email": userEmail,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))
	
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect session request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Printf("[Bridge Error] Connect Session Failed: %s", string(respBody))
		return "", fmt.Errorf("bridge connect error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("connect decode error: %w", err)
	}

	return result.URL, nil
}

// 3. Get Providers (Public App Auth - No Token)
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
		return nil, fmt.Errorf("unauthorized: check bridge_client_id/secret in render")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
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

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
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

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Resources []BridgeAccount `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Resources, nil
}

// 6. Refresh
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