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
	log.Println("🔐 Initializing Enable Banking Service...")
	
	appID := os.Getenv("ENABLE_BANKING_APP_ID")
	if appID == "" {
		log.Fatal("❌ ENABLE_BANKING_APP_ID environment variable is not set")
	}
	log.Printf("✅ App ID configured: %s...", appID[:8])
	
	privateKey := loadPrivateKey()
	
	return &EnableBankingService{
		BaseURL:    "https://api.enablebanking.com",
		AppID:      appID,
		PrivateKey: privateKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Load private key from file or environment variable
func loadPrivateKey() *rsa.PrivateKey {
	log.Println("🔑 Loading private key...")
	var pemData []byte
	
	// Option 1: Load from base64 environment variable (for production/Render)
	if base64Key := os.Getenv("ENABLE_BANKING_PRIVATE_KEY_BASE64"); base64Key != "" {
		log.Println("📦 Found ENABLE_BANKING_PRIVATE_KEY_BASE64 in environment")
		log.Printf("📏 Base64 key length: %d characters", len(base64Key))
		
		decoded, err := base64.StdEncoding.DecodeString(base64Key)
		if err != nil {
			log.Printf("❌ Failed to decode base64 private key: %v", err)
			log.Fatal("Base64 decoding failed - check if the key is properly encoded")
		}
		log.Printf("✅ Successfully decoded base64 key, PEM length: %d bytes", len(decoded))
		pemData = decoded
	} else if keyPath := os.Getenv("ENABLE_BANKING_PRIVATE_KEY_PATH"); keyPath != "" {
		// Option 2: Load from file (for local development)
		log.Printf("📁 Loading private key from file: %s", keyPath)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			log.Fatal("Failed to read private key file:", err)
		}
		log.Printf("✅ Successfully read key file, size: %d bytes", len(data))
		pemData = data
	} else {
		log.Fatal("❌ No private key configured. Set ENABLE_BANKING_PRIVATE_KEY_BASE64 or ENABLE_BANKING_PRIVATE_KEY_PATH")
	}

	// Parse PEM
	log.Println("🔍 Parsing PEM block...")
	block, _ := pem.Decode(pemData)
	if block == nil {
		log.Printf("❌ PEM data preview (first 100 chars): %s", string(pemData[:min(100, len(pemData))]))
		log.Fatal("Failed to parse PEM block - the data might not be in PEM format")
	}
	log.Printf("✅ PEM block type: %s", block.Type)

	// Parse private key - Try PKCS8 first (modern standard), then PKCS1
	log.Println("🔑 Parsing RSA private key...")
	
	// Try PKCS8 format first (standard for Enable Banking)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			log.Fatal("❌ Key is not an RSA private key")
		}
		log.Printf("✅ Successfully parsed PKCS8 private key, size: %d bits", privateKey.N.BitLen())
		return privateKey
	}
	
	log.Printf("⚠️  PKCS8 parsing failed: %v, trying PKCS1...", err)
	
	// Try PKCS1 format as fallback
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Printf("❌ PKCS1 parsing also failed: %v", err)
		log.Fatal("Failed to parse private key in both PKCS8 and PKCS1 formats")
	}
	
	log.Printf("✅ Successfully parsed PKCS1 private key, size: %d bits", privateKey.N.BitLen())
	return privateKey
}

// Generate JWT token signed with private key
// According to Enable Banking documentation:
// - Header must contain: kid (application ID)
// - Body must contain: iss = "enablebanking.com", aud = "api.enablebanking.com"
func (s *EnableBankingService) generateJWT() (string, error) {
	now := time.Now()
	
	// JWT Body/Claims - According to Enable Banking spec
	claims := jwt.MapClaims{
		"iss": "enablebanking.com",           // Issuer - MUST be "enablebanking.com"
		"aud": "api.enablebanking.com",       // Audience - MUST be "api.enablebanking.com"
		"iat": now.Unix(),                    // Issued at
		"exp": now.Add(5 * time.Minute).Unix(), // Expires in 5 minutes (max 24h)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	
	// JWT Header - Add the 'kid' (Key ID) which must be the Application ID
	// This is REQUIRED by Enable Banking API
	token.Header["kid"] = s.AppID
	
	signedToken, err := token.SignedString(s.PrivateKey)
	if err != nil {
		log.Printf("❌ JWT signing failed: %v", err)
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	log.Printf("✅ JWT token generated successfully with kid=%s (length: %d)", s.AppID[:8]+"...", len(signedToken))
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

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ========== 1. GET ASPSPs (Banks) ==========

// SandboxUser represents a test user in sandbox environment
type SandboxUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	OTP      string `json:"otp"`
}

// SandboxInfo contains sandbox environment information
type SandboxInfo struct {
	Users []SandboxUser `json:"users"`
}

// ASPSP represents a bank/financial institution
type ASPSP struct {
	Name        string       `json:"name"`
	Country     string       `json:"country"`
	BIC         string       `json:"bic,omitempty"`
	Logo        string       `json:"logo"`
	Sandbox     *SandboxInfo `json:"sandbox,omitempty"` // Pointer because it can be null/absent
	Beta        bool         `json:"beta"`
}

func (s *EnableBankingService) GetASPSPs(ctx context.Context, country string) ([]ASPSP, error) {
	url := s.BaseURL + "/aspsps"
	if country != "" {
		url += "?country=" + country
	}

	log.Printf("🌐 Fetching ASPSPs from: %s", url)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		log.Printf("❌ Failed to set headers: %v", err)
		return nil, err
	}

	log.Println("📤 Sending request to Enable Banking API...")
	resp, err := s.Client.Do(req)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("📥 Response status: %d", resp.StatusCode)

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ Failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		log.Printf("❌ API Error Response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("✅ Received response, size: %d bytes", len(respBody))
	log.Printf("📄 Response preview: %s", string(respBody[:min(200, len(respBody))]))

	// L'API retourne {"aspsps": [...]} et non directement un tableau
	var response struct {
		ASPSPs []ASPSP `json:"aspsps"`
	}
	
	if err := json.Unmarshal(respBody, &response); err != nil {
		log.Printf("❌ JSON parsing failed: %v", err)
		log.Printf("📄 Full response: %s", string(respBody))
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	log.Printf("✅ Successfully parsed %d ASPSPs", len(response.ASPSPs))
	return response.ASPSPs, nil
}

// ========== 2. CREATE AUTH REQUEST ==========

// Access defines the scope and validity of account access
type Access struct {
	ValidUntil string `json:"valid_until"` // RFC3339 format: "2025-12-24T23:59:59Z"
}

// ASPSPIdentifier identifies the bank for auth request
type ASPSPIdentifier struct {
	Name    string `json:"name"`
	Country string `json:"country"`
}

// AuthRequest is the request to create an authorization
type AuthRequest struct {
	Access      Access          `json:"access"`
	ASPSP       ASPSPIdentifier `json:"aspsp"`
	State       string          `json:"state"`
	RedirectURL string          `json:"redirect_url"`
	PSUType     string          `json:"psu_type"` // "personal" or "business"
}

type AuthResponse struct {
	AuthURL         string `json:"url"`
	State           string `json:"state"`
	AuthorizationID string `json:"authorization_id"`
}

func (s *EnableBankingService) CreateAuthRequest(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	log.Printf("🔐 Creating auth request for ASPSP: %s (%s)", req.ASPSP.Name, req.ASPSP.Country)

	body, _ := json.Marshal(req)
	log.Printf("📤 Auth request body: %s", string(body))
	
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/auth", bytes.NewBuffer(body))
	if err := s.setHeaders(httpReq); err != nil {
		log.Printf("❌ Failed to set headers: %v", err)
		return nil, err
	}

	log.Println("📤 Sending auth request to Enable Banking...")
	resp, err := s.Client.Do(httpReq)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("📥 Auth response status: %d", resp.StatusCode)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Printf("❌ Auth Request Error: %s", string(respBody))
		return nil, fmt.Errorf("auth request failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		log.Printf("❌ Failed to parse auth response: %v", err)
		return nil, err
	}

	log.Printf("✅ Auth URL generated: %s", authResp.AuthURL[:min(50, len(authResp.AuthURL))]+"...")
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
	log.Printf("🔐 Creating session with code and state")
	
	payload := SessionRequest{
		Code:  code,
		State: state,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/sessions", bytes.NewBuffer(body))
	if err := s.setHeaders(req); err != nil {
		log.Printf("❌ Failed to set headers: %v", err)
		return nil, err
	}

	log.Println("📤 Sending session creation request...")
	resp, err := s.Client.Do(req)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("📥 Session response status: %d", resp.StatusCode)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Printf("❌ Session Error: %s", string(respBody))
		return nil, fmt.Errorf("session creation failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var sessionResp SessionResponse
	if err := json.Unmarshal(respBody, &sessionResp); err != nil {
		log.Printf("❌ Failed to parse session response: %v", err)
		return nil, err
	}

	log.Printf("✅ Session created: %s with %d accounts", sessionResp.SessionID, len(sessionResp.Accounts))
	return &sessionResp, nil
}

// ========== 4. GET ACCOUNTS ==========

func (s *EnableBankingService) GetAccounts(ctx context.Context, sessionID string) ([]Account, error) {
	url := fmt.Sprintf("%s/sessions/%s/accounts", s.BaseURL, sessionID)
	log.Printf("🏦 Fetching accounts for session: %s", sessionID)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		log.Printf("❌ Failed to set headers: %v", err)
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	log.Printf("📥 Response status: %d", resp.StatusCode)

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var accounts []Account
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		log.Printf("❌ Failed to parse accounts: %v", err)
		return nil, err
	}

	log.Printf("✅ Retrieved %d accounts", len(accounts))
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
	url := fmt.Sprintf("%s/sessions/%s/accounts/%s/balances", s.BaseURL, sessionID, accountID)
	log.Printf("💰 Fetching balances for account: %s", accountID)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var balances []Balance
	if err := json.NewDecoder(resp.Body).Decode(&balances); err != nil {
		log.Printf("❌ Failed to parse balances: %v", err)
		return nil, err
	}

	log.Printf("✅ Retrieved %d balances", len(balances))
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
	
	log.Printf("💳 Fetching transactions for account: %s", accountID)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var transactions []Transaction
	if err := json.NewDecoder(resp.Body).Decode(&transactions); err != nil {
		log.Printf("❌ Failed to parse transactions: %v", err)
		return nil, err
	}

	log.Printf("✅ Retrieved %d transactions", len(transactions))
	return transactions, nil
}

// ========== 7. DELETE SESSION ==========

func (s *EnableBankingService) DeleteSession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s", s.BaseURL, sessionID)
	log.Printf("🗑️  Deleting session: %s", sessionID)
	
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err := s.setHeaders(req); err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Delete failed: %s", string(respBody))
		return fmt.Errorf("delete failed (%d): %s", resp.StatusCode, string(respBody))
	}

	log.Println("✅ Session deleted successfully")
	return nil
}