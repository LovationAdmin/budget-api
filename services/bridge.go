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
	// Utilisation de la version spécifiée dans votre code
	req.Header.Set("Bridge-Version", "2025-01-15") 
	req.Header.Set("Client-Id", s.ClientID)
	req.Header.Set("Client-Secret", s.ClientSecret)
}

// 1. Get or Create User & Generate Token
func (s *BridgeService) getOrCreateUserToken(ctx context.Context, userEmail string) (string, error) {
	externalID := hashEmail(userEmail)
	var userUUID string

	// --- STEP A: Check if user exists (List Users) ---
	listURL := fmt.Sprintf("%s/aggregation/users?external_user_id=%s", s.BaseURL, externalID)
	listReq, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	s.setHeaders(listReq)

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
		}
	}

	// --- STEP B: Create User if not found ---
	if userUUID == "" {
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
			b, _ := io.ReadAll(createResp.Body)
			log.Printf("[Bridge Error] Create User Failed: %s", string(b))
			if createResp.StatusCode == 409 {
				return "", fmt.Errorf("user conflict (409) - check bridge dashboard")
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
// MISE A JOUR : Accepte maintenant redirectURL
func (s *BridgeService) CreateConnectItem(ctx context.Context, userEmail string, redirectURL string) (string, error) {
	accessToken, err := s.getOrCreateUserToken(ctx, userEmail)
	if err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"user_email": userEmail,
	}
    // AJOUT : Ajoute l'URL de redirection si fournie
    if redirectURL != "" {
        payload["redirect_url"] = redirectURL
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

// 3. Get Providers
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

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		// Important: Log body to debug
		log.Printf("[Bridge Error] Get Accounts: %s", string(b))
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
		b, _ := io.ReadAll(resp.Body)
		log.Printf("[Bridge Error] Refresh: %s", string(b))
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	return nil
}

// 7. Get Transactions (Optimized: Last 40 Days + Pagination)
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

	// Calculer la date d'il y a 40 jours (Format ISO 8601 Requis par Bridge)
	sinceDate := time.Now().AddDate(0, 0, -40).Format(time.RFC3339)

	var allTransactions []BridgeTransaction
	
	// URL initiale avec filtre de date
	nextURI := fmt.Sprintf("/aggregation/transactions?limit=50&sort=-date&since=%s", sinceDate)

	// Boucle de pagination
	for nextURI != "" {
		fullURL := s.BaseURL + strings.TrimPrefix(nextURI, "/v3")
		if strings.HasPrefix(nextURI, "http") {
			fullURL = nextURI
		}

		req, _ := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		s.setHeaders(req)

		resp, err := s.Client.Do(req)
		if err != nil {
			return nil, err
		}
		
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != 200 {
			log.Printf("[Bridge Error] Transactions: %s", string(bodyBytes))
			return nil, fmt.Errorf("bridge error %d", resp.StatusCode)
		}

		var result struct {
			Resources  []BridgeTransaction `json:"resources"`
			Pagination struct {
				NextURI *string `json:"next_uri"` // Pointeur pour gérer null
			} `json:"pagination"`
		}
		
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			return nil, err
		}

		allTransactions = append(allTransactions, result.Resources...)

		if result.Pagination.NextURI != nil && *result.Pagination.NextURI != "" && *result.Pagination.NextURI != "null" {
			nextURI = *result.Pagination.NextURI
		} else {
			nextURI = ""
		}
	}

	return allTransactions, nil
}