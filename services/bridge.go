package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings" // Added for trimming
	"time"
)

type BridgeService struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
	Client       *http.Client
}

func NewBridgeService() *BridgeService {
    // SECURITY FIX: Trim spaces to prevent 401 errors from bad copy-paste
	return &BridgeService{
		ClientID:     strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_SECRET")),
		BaseURL:      "https://api.bridgeapi.io/v3",
		Client:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *BridgeService) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Bridge-Version", "2025-01-15") // Ensure this matches your Dashboard version
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// 1. Get/Create User and Generate Token
func (s *BridgeService) getOrCreateUserToken(ctx context.Context, userEmail string) (string, error) {
	// STEP A: Create or Find User via external_user_id
    // Documentation: "You can optionally include an external_user_id" 
	createPayload := map[string]string{
        "external_user_id": userEmail, // We use email as the stable ID
    }
	body, _ := json.Marshal(createPayload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/users", bytes.NewBuffer(body))
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connection error during user creation: %w", err)
	}
	defer resp.Body.Close()

    // Handle 401 specifically
    if resp.StatusCode == 401 {
        return "", fmt.Errorf("BRIDGE 401 UNAUTHORIZED: Check Client-ID/Secret in Render. ID used: %s...", s.ClientID[:10])
    }

	var userUUID string

	if resp.StatusCode == 201 {
		// User Created
		var res struct { Uuid string `json:"uuid"` }
		json.NewDecoder(resp.Body).Decode(&res)
		userUUID = res.Uuid
	} else if resp.StatusCode == 409 {
		// User Exists -> List users to find UUID
		listReq, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/users?external_user_id="+userEmail, nil)
		s.setHeaders(listReq)
		listResp, _ := s.Client.Do(listReq)
		defer listResp.Body.Close()
        
        var listRes struct { Resources []struct { Uuid string `json:"uuid"` } `json:"resources"` }
        json.NewDecoder(listResp.Body).Decode(&listRes)
        
        if len(listRes.Resources) > 0 {
            userUUID = listRes.Resources[0].Uuid
        }
	} else {
        bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bridge user error (%d): %s", resp.StatusCode, string(bodyBytes))
    }

	if userUUID == "" {
		return "", fmt.Errorf("could not retrieve user_uuid for %s", userEmail)
	}

	// STEP B: Generate Token
    // Documentation: "Generate authorization tokens by sending a POST request" 
	authPayload := map[string]string{"user_uuid": userUUID}
	authBody, _ := json.Marshal(authPayload)
	authReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/authorization/token", bytes.NewBuffer(authBody))
	s.setHeaders(authReq)

	authResp, err := s.Client.Do(authReq)
	if err != nil {
		return "", err
	}
	defer authResp.Body.Close()

	if authResp.StatusCode != 200 {
        bodyBytes, _ := io.ReadAll(authResp.Body)
		return "", fmt.Errorf("bridge token error (%d): %s", authResp.StatusCode, string(bodyBytes))
	}

	var tokenRes struct { AccessToken string `json:"access_token"` }
	if err := json.NewDecoder(authResp.Body).Decode(&tokenRes); err != nil {
		return "", fmt.Errorf("failed to decode token: %w", err)
	}

	return tokenRes.AccessToken, nil
}

// 2. Create Connect Session
func (s *BridgeService) CreateConnectItem(ctx context.Context, userEmail string) (string, error) {
	// Authenticate First
    accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
    if err != nil {
        return "", err
    }

    // Create Link
    // Documentation: "prefill_email becomes user_email" 
	payload := map[string]interface{}{
		"user_email": userEmail,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))
	
    // User-level endpoints need Bearer Token
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

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

// 3. Get Providers (Public App Auth)
type BridgeProvider struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	CountryCode string `json:"country_code"`
	Images      struct {
		Logo string `json:"logo"`
	} `json:"images"`
}

func (s *BridgeService) GetBanks(ctx context.Context) ([]BridgeProvider, error) {
    // Documentation: "country becomes country_code" 
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/providers?country_code=FR", nil)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
        // Helpful debug for 401
        if resp.StatusCode == 401 {
            return nil, fmt.Errorf("bridge 401 unauthorized (check client id/secret)")
        }
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

// 4. Get Items
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
	if err != nil { return nil, err }
	defer resp.Body.Close()

	if resp.StatusCode != 200 { return nil, fmt.Errorf("status %d", resp.StatusCode) }

	var result struct { Resources []BridgeItem `json:"resources"` }
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Resources, nil
}

// 5. Get Accounts
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
	if err != nil { return nil, err }
	defer resp.Body.Close()

	if resp.StatusCode != 200 { return nil, fmt.Errorf("status %d", resp.StatusCode) }

	var result struct { Resources []BridgeAccount `json:"resources"` }
	json.NewDecoder(resp.Body).Decode(&result)
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
	if err != nil { return err }
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 202 { return fmt.Errorf("status %d", resp.StatusCode) }
	return nil
}