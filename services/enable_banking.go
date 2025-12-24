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

func loadPrivateKey() *rsa.PrivateKey {
	log.Println("🔑 Loading private key...")
	var pemData []byte
	
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

	log.Println("🔍 Parsing PEM block...")
	block, _ := pem.Decode(pemData)
	if block == nil {
		log.Printf("❌ PEM data preview (first 100 chars): %s", string(pemData[:min(100, len(pemData))]))
		log.Fatal("Failed to parse PEM block - the data might not be in PEM format")
	}
	log.Printf("✅ PEM block type: %s", block.Type)

	log.Println("🔑 Parsing RSA private key...")
	
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
	
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Printf("❌ PKCS1 parsing also failed: %v", err)
		log.Fatal("Failed to parse private key in both PKCS8 and PKCS1 formats")
	}
	
	log.Printf("✅ Successfully parsed PKCS1 private key, size: %d bits", privateKey.N.BitLen())
	return privateKey
}

func (s *EnableBankingService) generateJWT() (string, error) {
	now := time.Now()
	
	claims := jwt.MapClaims{
		"iss": "enablebanking.com",
		"aud": "api.enablebanking.com",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.AppID
	
	signedToken, err := token.SignedString(s.PrivateKey)
	if err != nil {
		log.Printf("❌ JWT signing failed: %v", err)
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}

	log.Printf("✅ JWT token generated successfully with kid=%s (length: %d)", s.AppID[:8]+"...", len(signedToken))
	return signedToken, nil
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ========== STRUCTS ==========

type SandboxUser struct {
	Username string `json:"username"`
	Password string `json:"password"`
	OTP      string `json:"otp"`
}

type SandboxInfo struct {
	Users []SandboxUser `json:"users"`
}

type ASPSP struct {
	Name        string       `json:"name"`
	Country     string       `json:"country"`
	BIC         string       `json:"bic,omitempty"`
	Logo        string       `json:"logo"`
	Sandbox     *SandboxInfo `json:"sandbox,omitempty"`
	Beta        bool         `json:"beta"`
}

type Access struct {
	ValidUntil string `json:"valid_until"`
}

type ASPSPIdentifier struct {
	Name    string `json:"name"`
	Country string `json:"country"`
}

type AuthRequest struct {
	Access      Access          `json:"access"`
	ASPSP       ASPSPIdentifier `json:"aspsp"`
	State       string          `json:"state"`
	RedirectURL string          `json:"redirect_url"`
	PSUType     string          `json:"psu_type"`
}

type AuthResponse struct {
	AuthURL string `json:"url"`
	State   string `json:"authorization_id"`
}

type SessionRequest struct {
	Code  string `json:"code"`
	State string `json:"state,omitempty"`
}

type AccountIdentification struct {
	IBAN  string `json:"iban,omitempty"`
	Other *struct {
		Identification string `json:"identification"`
		SchemeName     string `json:"scheme_name"`
	} `json:"other,omitempty"`
}

type Account struct {
	AccountID       AccountIdentification `json:"account_id"`
	Name            string                `json:"name"`
	Currency        string                `json:"currency"`
	CashAccountType string                `json:"cash_account_type"`
	Details         string                `json:"details,omitempty"`
	Product         string                `json:"product,omitempty"`
	UID             string                `json:"uid,omitempty"`
}

type SessionResponse struct {
	SessionID string    `json:"session_id"`
	Accounts  []Account `json:"accounts"`
	ASPSP     struct {
		Name    string `json:"name"`
		Country string `json:"country"`
	} `json:"aspsp"`
	PSUType string `json:"psu_type"`
}

type AmountType struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

type Balance struct {
	Name          string     `json:"name"`
	BalanceAmount AmountType `json:"balance_amount"`
	BalanceType   string     `json:"balance_type"`
	ReferenceDate string     `json:"reference_date,omitempty"`
}

type BalancesResponse struct {
	Balances []Balance `json:"balances"`
}

// TRANSACTION STRUCTURES pour le mapping
type Transaction struct {
	EntryReference        string     `json:"entry_reference,omitempty"`
	TransactionID         string     `json:"transaction_id,omitempty"`
	TransactionAmount     AmountType `json:"transaction_amount"`
	CreditDebitIndicator  string     `json:"credit_debit_indicator"`
	Status                string     `json:"status"`
	BookingDate           string     `json:"booking_date,omitempty"`
	ValueDate             string     `json:"value_date,omitempty"`
	TransactionDate       string     `json:"transaction_date,omitempty"`
	CreditorName          string     `json:"creditor,omitempty"`
	DebtorName            string     `json:"debtor,omitempty"`
	RemittanceInfo        string     `json:"remittance_information,omitempty"`
	BalanceAfterTransaction AmountType `json:"balance_after_transaction,omitempty"`
}

type TransactionsResponse struct {
	Transactions []Transaction `json:"transactions"`
	ContinuationKey string `json:"continuation_key,omitempty"`
}

// ========== API METHODS ==========

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

	var response struct {
		ASPSPs []ASPSP `json:"aspsps"`
	}
	
	if err := json.Unmarshal(respBody, &response); err != nil {
		log.Printf("❌ JSON parsing failed: %v", err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	log.Printf("✅ Successfully parsed %d ASPSPs", len(response.ASPSPs))
	return response.ASPSPs, nil
}

func (s *EnableBankingService) CreateAuthRequest(ctx context.Context, req AuthRequest) (*AuthResponse, error) {
	log.Printf("🔐 Creating auth request for %s (%s)", req.ASPSP.Name, req.ASPSP.Country)
	
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/auth", bytes.NewBuffer(body))
	if err := s.setHeaders(httpReq); err != nil {
		log.Printf("❌ Failed to set headers: %v", err)
		return nil, err
	}

	log.Println("📤 Sending auth request...")
	resp, err := s.Client.Do(httpReq)
	if err != nil {
		log.Printf("❌ HTTP request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("📥 Auth response status: %d", resp.StatusCode)

	if resp.StatusCode != 200 {
		log.Printf("❌ Auth Error: %s", string(respBody))
		return nil, fmt.Errorf("auth request failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		log.Printf("❌ Failed to parse auth response: %v", err)
		return nil, err
	}

	log.Printf("✅ Auth URL generated")
	return &authResp, nil
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
	
	for i, acc := range sessionResp.Accounts {
		iban := acc.AccountID.IBAN
		if iban == "" && acc.AccountID.Other != nil {
			iban = acc.AccountID.Other.Identification
		}
		log.Printf("   📊 Account %d: %s | IBAN: %s | UID: %s", i+1, acc.Name, iban, acc.UID)
	}
	
	return &sessionResp, nil
}

func (s *EnableBankingService) GetBalances(ctx context.Context, sessionID, accountUID string) ([]Balance, error) {
	url := fmt.Sprintf("%s/accounts/%s/balances", s.BaseURL, accountUID)
	log.Printf("💰 Fetching balances for account UID: %s", accountUID)
	
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

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("📥 Balances response status: %d", resp.StatusCode)

	if resp.StatusCode != 200 {
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var balancesResp BalancesResponse
	if err := json.Unmarshal(respBody, &balancesResp); err != nil {
		log.Printf("❌ Failed to parse balances: %v", err)
		return nil, err
	}

	log.Printf("✅ Retrieved %d balances", len(balancesResp.Balances))
	for i, bal := range balancesResp.Balances {
		log.Printf("   💰 Balance %d: %s = %s %s", i+1, bal.Name, bal.BalanceAmount.Amount, bal.BalanceAmount.Currency)
	}
	
	return balancesResp.Balances, nil
}

func (s *EnableBankingService) GetTransactions(ctx context.Context, sessionID, accountUID string, dateFrom, dateTo string) ([]Transaction, error) {
	url := fmt.Sprintf("%s/accounts/%s/transactions", s.BaseURL, accountUID)
	if dateFrom != "" && dateTo != "" {
		url += fmt.Sprintf("?date_from=%s&date_to=%s", dateFrom, dateTo)
	}
	
	log.Printf("💳 Fetching transactions for account: %s", accountUID)

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

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var transResp TransactionsResponse
	if err := json.Unmarshal(respBody, &transResp); err != nil {
		log.Printf("❌ Failed to parse transactions: %v", err)
		return nil, err
	}

	log.Printf("✅ Retrieved %d transactions", len(transResp.Transactions))
	return transResp.Transactions, nil
}

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