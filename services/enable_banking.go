package services

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type EnableBankingService struct {
	BaseURL    string
	AppID      string
	PrivateKey *rsa.PrivateKey
	Client     *http.Client
}

func NewEnableBankingService() *EnableBankingService {
	privateKey := loadPrivateKey()
	
	return &EnableBankingService{
		BaseURL:    "https://api.enablebanking.com",
		AppID:      os.Getenv("ENABLE_BANKING_APP_ID"),
		PrivateKey: privateKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Load private key from file or environment variable
func loadPrivateKey() *rsa.PrivateKey {
	var pemData []byte
	
	// Option 1: Load from base64 environment variable (for production/Render)
	if base64Key := os.Getenv("ENABLE_BANKING_PRIVATE_KEY_BASE64"); base64Key != "" {
		decoded, err := base64.StdEncoding.DecodeString(base64Key)
		if err != nil {
			log.Fatal("Failed to decode base64 private key:", err)
		}
		pemData = decoded
	} else if keyPath := os.Getenv("ENABLE_BANKING_PRIVATE_KEY_PATH"); keyPath != "" {
		// Option 2: Load from file (for local development)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			log.Fatal("Failed to read private key file:", err)
		}
		pemData = data
	} else {
		log.Fatal("No private key configured. Set ENABLE_BANKING_PRIVATE_KEY_BASE64 or ENABLE_BANKING_PRIVATE_KEY_PATH")
	}

	// Parse PEM
	block, _ := pem.Decode(pemData)
	if block == nil {
		log.Fatal("Failed to parse PEM block containing the private key")
	}

	// Parse private key
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format
		key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			log.Fatal("Failed to parse private key:", err, err2)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			log.Fatal("Not an RSA private key")
		}
	}

	return privateKey
}

// Generate JWT token signed with private key
func (s *EnableBankingService) generateJWT() (string, error) {
	now := time.Now()
	
	claims := jwt.MapClaims{
		"iss": s.AppID,                    // Issuer = Application ID
		"aud": "https://api.enablebanking.com", // Audience
		"iat": now.Unix(),                 // Issued at
		"exp": now.Add(5 * time.Minute).Unix(), // Expires in 5 minutes
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	
	signedToken, err := token.SignedString(s.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	return signedToken, nil
}

// Common headers with JWT authentication
func (s *EnableBankingService) setHeaders(req *http.Request) error {
	jwtToken, err := s.generateJWT()
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

// ========== 1. GET ASPSPs (Banks) ==========

type ASPSP struct {
	ID          string `json:"aspsp_id"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	BIC         string `json:"bic"`
	LogoURL     string `json:"logo_url"`
	Sandbox     bool   `json:"sandbox"`
	AISSupport  bool   `json:"ais_support"`
	PISSupport  bool   `json:"pis_support"`
}

func (s *EnableBankingService) GetASPSPs(ctx context.Context, country string) ([]ASPSP, error) {
	url := s.BaseURL + "/aspsps"
	if country != "" {
		url += "?country=" + country
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var aspsps []ASPSP
	if err := json.NewDecoder(resp.Body).Decode(&aspsps); err != nil {
		return nil, err
	}

	return aspsps, nil
}

// ========== 2. CREATE AUTH REQUEST ==========

type AuthRequest struct {
	RedirectURL string   `json:"redirect_url"`
	ASPSPID     string   `json:"aspsp_id"`
	State       string   `json:"state"`
	Access      []string `json:"access"`
}

type AuthResponse struct {
	AuthURL string `json:"url"`
	State   string `json:"state"`
}

func (s *EnableBankingService) CreateAuthRequest(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	if req.Access == nil {
		req.Access = []string{"accounts", "balances", "transactions"}
	}

	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/auth", bytes.NewBuffer(body))
	if err := s.setHeaders(httpReq); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Printf("[Enable Banking Error] Auth Request: %s", string(respBody))
		return nil, fmt.Errorf("auth request failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return nil, err
	}

	return &authResp, nil
}

// ========== 3. CREATE SESSION ==========

type SessionRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type SessionResponse struct {
	SessionID string    `json:"session_id"`
	Accounts  []Account `json:"accounts"`
}

type Account struct {
	AccountID   string  `json:"account_id"`
	IBAN        string  `json:"iban"`
	Name        string  `json:"name"`
	Currency    string  `json:"currency"`
	Balance     float64 `json:"balance"`
	AccountType string  `json:"account_type"`
}

func (s *EnableBankingService) CreateSession(ctx context.Context, code, state string) (*SessionResponse, error) {
	payload := SessionRequest{
		Code:  code,
		State: state,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/sessions", bytes.NewBuffer(body))
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Printf("[Enable Banking Error] Session: %s", string(respBody))
		return nil, fmt.Errorf("session creation failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var sessionResp SessionResponse
	if err := json.Unmarshal(respBody, &sessionResp); err != nil {
		return nil, err
	}

	return &sessionResp, nil
}

// ========== 4. GET ACCOUNTS ==========

func (s *EnableBankingService) GetAccounts(ctx context.Context, sessionID string) ([]Account, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/sessions/%s/accounts", s.BaseURL, sessionID), nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var accounts []Account
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return nil, err
	}

	return accounts, nil
}

// ========== 5. GET BALANCES ==========

type Balance struct {
	AccountID     string  `json:"account_id"`
	BalanceAmount float64 `json:"balance_amount"`
	BalanceType   string  `json:"balance_type"`
	Currency      string  `json:"currency"`
	ReferenceDate string  `json:"reference_date"`
}

func (s *EnableBankingService) GetBalances(ctx context.Context, sessionID, accountID string) ([]Balance, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/sessions/%s/accounts/%s/balances", s.BaseURL, sessionID, accountID), nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var balances []Balance
	if err := json.NewDecoder(resp.Body).Decode(&balances); err != nil {
		return nil, err
	}

	return balances, nil
}

// ========== 6. GET TRANSACTIONS ==========

type Transaction struct {
	TransactionID     string  `json:"transaction_id"`
	AccountID         string  `json:"account_id"`
	BookingDate       string  `json:"booking_date"`
	ValueDate         string  `json:"value_date"`
	TransactionAmount float64 `json:"transaction_amount"`
	Currency          string  `json:"currency"`
	DebtorName        string  `json:"debtor_name"`
	CreditorName      string  `json:"creditor_name"`
	RemittanceInfo    string  `json:"remittance_information"`
}

func (s *EnableBankingService) GetTransactions(ctx context.Context, sessionID, accountID string, dateFrom, dateTo string) ([]Transaction, error) {
	url := fmt.Sprintf("%s/sessions/%s/accounts/%s/transactions", s.BaseURL, sessionID, accountID)
	if dateFrom != "" && dateTo != "" {
		url += fmt.Sprintf("?date_from=%s&date_to=%s", dateFrom, dateTo)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var transactions []Transaction
	if err := json.NewDecoder(resp.Body).Decode(&transactions); err != nil {
		return nil, err
	}

	return transactions, nil
}

// ========== 7. DELETE SESSION ==========

func (s *EnableBankingService) DeleteSession(ctx context.Context, sessionID string) error {
	req, _ := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("%s/sessions/%s", s.BaseURL, sessionID), nil)
	if err := s.setHeaders(req); err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}