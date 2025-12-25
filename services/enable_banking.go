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

// ============================================================================
// SERVICE STRUCTURE
// ============================================================================

type EnableBankingService struct {
	BaseURL    string
	AppID      string
	PrivateKey *rsa.PrivateKey
	Client     *http.Client
}

// ============================================================================
// INITIALIZATION
// ============================================================================

func NewEnableBankingService() *EnableBankingService {
	log.Println("🔐 Initializing Enable Banking Service...")
	
	appID := os.Getenv("ENABLE_BANKING_APP_ID")
	if appID == "" {
		log.Fatal("❌ ENABLE_BANKING_APP_ID environment variable is not set")
	}
	log.Printf("✅ App ID configured: %s...", appID[:min(8, len(appID))])
	
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
	
	// Essayer base64 d'abord
	if base64Key := os.Getenv("ENABLE_BANKING_PRIVATE_KEY_BASE64"); base64Key != "" {
		log.Println("📦 Found ENABLE_BANKING_PRIVATE_KEY_BASE64")
		
		decoded, err := base64.StdEncoding.DecodeString(base64Key)
		if err != nil {
			log.Fatalf("❌ Failed to decode base64 private key: %v", err)
		}
		log.Printf("✅ Successfully decoded base64 key, size: %d bytes", len(decoded))
		pemData = decoded
	} else if keyPath := os.Getenv("ENABLE_BANKING_PRIVATE_KEY_PATH"); keyPath != "" {
		log.Printf("📁 Loading private key from file: %s", keyPath)
		data, err := os.ReadFile(keyPath)
		if err != nil {
			log.Fatalf("❌ Failed to read private key file: %v", err)
		}
		log.Printf("✅ Successfully read key file, size: %d bytes", len(data))
		pemData = data
	} else {
		log.Fatal("❌ No private key configured. Set ENABLE_BANKING_PRIVATE_KEY_BASE64 or ENABLE_BANKING_PRIVATE_KEY_PATH")
	}

	// Parse PEM
	block, _ := pem.Decode(pemData)
	if block == nil {
		log.Fatal("❌ Failed to parse PEM block")
	}
	log.Printf("✅ PEM block type: %s", block.Type)

	// Parse RSA key
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		privateKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			log.Fatal("❌ Key is not RSA private key")
		}
		log.Println("✅ Successfully parsed PKCS8 RSA private key")
		return privateKey
	}

	// Fallback PKCS1
	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Fatalf("❌ Failed to parse private key: %v", err)
	}
	log.Println("✅ Successfully parsed PKCS1 RSA private key")
	return privateKey
}

// ============================================================================
// JWT GENERATION
// ============================================================================

func (s *EnableBankingService) generateJWT() (string, error) {
	now := time.Now()
	
	claims := jwt.MapClaims{
		"iss": "enablebanking.com",
		"aud": "api.enablebanking.com",
		"iat": now.Unix(),
		"exp": now.Add(1 * time.Hour).Unix(),
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = s.AppID
	
	signed, err := token.SignedString(s.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign JWT: %w", err)
	}
	
	return signed, nil
}

func (s *EnableBankingService) setHeaders(req *http.Request) error {
	token, err := s.generateJWT()
	if err != nil {
		return err
	}
	
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

// ============================================================================
// DATA STRUCTURES
// ============================================================================

type SandboxUser struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	OTP      string `json:"otp,omitempty"`
}

type SandboxUser struct {
    Username string `json:"username,omitempty"`
    Password string `json:"password,omitempty"`
    OTP      string `json:"otp,omitempty"`
}

type ASPSP struct {
    Name    string `json:"name"`
    Country string `json:"country"`
    Logo    string `json:"logo"`
    BIC     string `json:"bic,omitempty"`
    Sandbox *struct {
        Users []SandboxUser `json:"users"`  // ✅ CORRECT
    } `json:"sandbox,omitempty"`
    Beta bool `json:"beta"`
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
	URL             string `json:"url"`
	AuthorizationID string `json:"authorization_id"`
	PSUIDHash       string `json:"psu_id_hash"`
}

type SessionRequest struct {
	Code string `json:"code"`
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
	UID             string                `json:"uid"`
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

type Transaction struct {
	EntryReference          string     `json:"entry_reference,omitempty"`
	TransactionID           string     `json:"transaction_id,omitempty"`
	TransactionAmount       AmountType `json:"transaction_amount"`
	CreditDebitIndicator    string     `json:"credit_debit_indicator"`
	Status                  string     `json:"status"`
	BookingDate             string     `json:"booking_date,omitempty"`
	ValueDate               string     `json:"value_date,omitempty"`
	TransactionDate         string     `json:"transaction_date,omitempty"`
	RemittanceInformation   []string   `json:"remittance_information,omitempty"`
	BalanceAfterTransaction AmountType `json:"balance_after_transaction,omitempty"`
	Creditor                *struct {
		Name string `json:"name"`
	} `json:"creditor,omitempty"`
	Debtor *struct {
		Name string `json:"name"`
	} `json:"debtor,omitempty"`
}

type TransactionsResponse struct {
	Transactions    []Transaction `json:"transactions"`
	ContinuationKey string        `json:"continuation_key,omitempty"`
}

// ============================================================================
// API METHODS
// ============================================================================

// GetASPSPs récupère la liste des banques disponibles
func (s *EnableBankingService) GetASPSPs(ctx context.Context, country string) ([]ASPSP, error) {
	url := s.BaseURL + "/aspsps"
	if country != "" {
		url += "?country=" + country + "&psu_type=personal&service=AIS"
	}

	log.Printf("🌐 Fetching ASPSPs from: %s", url)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, fmt.Errorf("failed to set headers: %w", err)
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("❌ API Error Response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ASPSPs []ASPSP `json:"aspsps"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("✅ Found %d ASPSPs", len(result.ASPSPs))
	return result.ASPSPs, nil
}

// CreateAuthRequest crée une demande d'autorisation
func (s *EnableBankingService) CreateAuthRequest(ctx context.Context, authReq AuthRequest) (*AuthResponse, error) {
	log.Printf("🔐 Creating auth request for %s (%s)", authReq.ASPSP.Name, authReq.ASPSP.Country)

	body, _ := json.Marshal(authReq)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/auth", bytes.NewBuffer(body))
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	log.Printf("📥 Auth response status: %d", resp.StatusCode)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		log.Printf("❌ Auth Error: %s", string(respBody))
		return nil, fmt.Errorf("auth failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var authResp AuthResponse
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("✅ Authorization URL created: %s", authResp.URL)
	return &authResp, nil
}

// CreateSession crée une session après autorisation
func (s *EnableBankingService) CreateSession(ctx context.Context, code, state string) (*SessionResponse, error) {
	log.Printf("🔄 Creating session with code: %s...", code[:min(10, len(code))])

	sessionReq := SessionRequest{Code: code}
	body, _ := json.Marshal(sessionReq)

	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/sessions", bytes.NewBuffer(body))
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
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
		return nil, fmt.Errorf("failed to parse response: %w", err)
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

// GetBalances récupère les soldes d'un compte
func (s *EnableBankingService) GetBalances(ctx context.Context, sessionID, accountUID string) ([]Balance, error) {
	url := fmt.Sprintf("%s/accounts/%s/balances", s.BaseURL, accountUID)
	log.Printf("💰 Fetching balances for account UID: %s", accountUID)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var balancesResp BalancesResponse
	if err := json.Unmarshal(respBody, &balancesResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("✅ Retrieved %d balances", len(balancesResp.Balances))
	for i, bal := range balancesResp.Balances {
		log.Printf("   💰 Balance %d: %s = %s %s", i+1, bal.Name, bal.BalanceAmount.Amount, bal.BalanceAmount.Currency)
	}
	
	return balancesResp.Balances, nil
}

// GetTransactions récupère les transactions d'un compte
func (s *EnableBankingService) GetTransactions(ctx context.Context, accountUID string, dateFrom, dateTo string) ([]Transaction, error) {
	url := fmt.Sprintf("%s/accounts/%s/transactions", s.BaseURL, accountUID)
	if dateFrom != "" && dateTo != "" {
		url += fmt.Sprintf("?date_from=%s&date_to=%s", dateFrom, dateTo)
	}
	
	log.Printf("💳 Fetching transactions for account: %s (from %s to %s)", accountUID, dateFrom, dateTo)

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err := s.setHeaders(req); err != nil {
		return nil, err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		log.Printf("❌ Error response: %s", string(respBody))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var transResp TransactionsResponse
	if err := json.Unmarshal(respBody, &transResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	log.Printf("✅ Retrieved %d transactions", len(transResp.Transactions))
	return transResp.Transactions, nil
}

// DeleteSession supprime une session
func (s *EnableBankingService) DeleteSession(ctx context.Context, sessionID string) error {
	url := fmt.Sprintf("%s/sessions/%s", s.BaseURL, sessionID)
	log.Printf("🗑️  Deleting session: %s", sessionID)
	
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err := s.setHeaders(req); err != nil {
		return err
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
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

// ============================================================================
// UTILITY FUNCTIONS
// ============================================================================

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}