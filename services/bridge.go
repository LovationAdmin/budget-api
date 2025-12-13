package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	return &BridgeService{
		ClientID:     strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("BRIDGE_CLIENT_SECRET")),
		BaseURL:      "https://api.bridgeapi.io/v3",
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
	req.Header.Set("Bridge-Version", "2021-06-01") // Version stable
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// 1. Get or Create User & Generate Token
func (s *BridgeService) getOrCreateUserToken(ctx context.Context, userEmail string) (string, error) {
	externalID := hashEmail(userEmail)
	var userUUID string

	// A. Check Existence
	listURL := fmt.Sprintf("%s/aggregation/users?external_user_id=%s", s.BaseURL, externalID)
	listReq, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	s.setHeaders(listReq)

	listResp, err := s.Client.Do(listReq)
	if err != nil {
		return "", fmt.Errorf("connection error: %w", err)
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
		}
	}

	// B. Create if not found
	if userUUID == "" {
		createPayload := map[string]string{"external_user_id": externalID}
		body, _ := json.Marshal(createPayload)
		createReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/users", bytes.NewBuffer(body))
		s.setHeaders(createReq)

		createResp, err := s.Client.Do(createReq)
		if err != nil {
			return "", err
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
			return "", fmt.Errorf("failed to create bridge user")
		}
	}

	// C. Generate Token
	authPayload := map[string]string{"user_uuid": userUUID}
	authBody, _ := json.Marshal(authPayload)
	authReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/authorization/token", bytes.NewBuffer(authBody))
	s.setHeaders(authReq)

	authResp, err := s.Client.Do(authReq)
	if err != nil {
		return "", err
	}
	defer authResp.Body.Close()

	var tokenRes struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(authResp.Body).Decode(&tokenRes); err != nil {
		return "", err
	}

	return tokenRes.AccessToken, nil
}

// 2. Create Connect Session
func (s *BridgeService) CreateConnectItem(ctx context.Context, userEmail string) (string, error) {
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{"user_email": userEmail} // Pre-fill email for UI
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/aggregation/connect-sessions", bytes.NewBuffer(body))
	
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.URL, nil
}

// 3. Get Providers (Public)
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
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Resources []BridgeItem `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Resources, nil
}

// 5. Get Accounts
type BridgeAccount struct {
	ID       int64   `json:"id"`
	ItemID   int64   `json:"item_id"`
	Name     string  `json:"name"`
	Balance  float64 `json:"balance"`
	Currency string  `json:"currency_code"`
	IBAN     string  `json:"iban"`
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
	s.Client.Do(req)
	return nil
}

// 7. Get Transactions (Last 40 Days Logic)
type BridgeTransaction struct {
	ID          int64   `json:"id"`
	AccountID   int64   `json:"account_id"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency_code"`
	Description string  `json:"clean_description"`
	Date        string  `json:"date"` // YYYY-MM-DD
}

func (s *BridgeService) GetTransactions(ctx context.Context, userEmail string, accountIDs []string) ([]BridgeTransaction, error) {
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return nil, err
	}

	// Fetch recent transactions (limit 100 to cover 40 days easily)
	req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/aggregation/transactions?limit=100&sort=-date", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bridge error %d", resp.StatusCode)
	}

	var result struct {
		Resources []BridgeTransaction `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// Filter Logic: Last 40 Days Only
	var recentTransactions []BridgeTransaction
	cutoffDate := time.Now().AddDate(0, 0, -40) // 40 days ago

	for _, tx := range result.Resources {
		// Parse transaction date
		txDate, err := time.Parse("2006-01-02", tx.Date)
		if err == nil && txDate.After(cutoffDate) {
			// Keep it if it's recent
			recentTransactions = append(recentTransactions, tx)
		}
	}

	return recentTransactions, nil
}