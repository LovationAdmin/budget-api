package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

type EnableBankingService struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	Client       *http.Client
}

func NewEnableBankingService() *EnableBankingService {
	return &EnableBankingService{
		BaseURL:      "https://api.enablebanking.com",
		ClientID:     os.Getenv("ENABLE_BANKING_CLIENT_ID"),
		ClientSecret: os.Getenv("ENABLE_BANKING_CLIENT_SECRET"),
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Common headers
func (s *EnableBankingService) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
}

// ========== 1. AUTHENTICATION ==========

type AuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Get access token for Enable Banking API
func (s *EnableBankingService) GetAccessToken(ctx context.Context) (string, error) {
	payload := map[string]string{
		"client_id":     s.ClientID,
		"client_secret": s.ClientSecret,
		"grant_type":    "client_credentials",
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/auth/token", bytes.NewBuffer(body))
	s.setHeaders(req)

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[Enable Banking Error] Auth failed: %s", string(respBody))
		return "", fmt.Errorf("auth failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var authResp AuthResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("decode error: %w", err)
	}

	return authResp.AccessToken, nil
}

// ========== 2. GET ASPSPs (Banks) ==========

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

// Get list of available banks (ASPSPs)
func (s *EnableBankingService) GetASPSPs(ctx context.Context, country string) ([]ASPSP, error) {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := s.BaseURL + "/aspsps"
	if country != "" {
		url += "?country=" + country
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	var aspsps []ASPSP
	if err := json.NewDecoder(resp.Body).Decode(&aspsps); err != nil {
		return nil, err
	}

	return aspsps, nil
}

// ========== 3. CREATE AUTH REQUEST ==========

type AuthRequest struct {
	RedirectURL string   `json:"redirect_url"`
	ASPSPID     string   `json:"aspsp_id"`
	State       string   `json:"state"`
	Access      []string `json:"access"` // ["accounts", "balances", "transactions"]
}

type AuthResponse2 struct {
	AuthURL string `json:"url"`
	State   string `json:"state"`
}

// Create authorization request to connect a bank
func (s *EnableBankingService) CreateAuthRequest(ctx context.Context, req AuthRequest) (*AuthResponse2, error) {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	if req.Access == nil {
		req.Access = []string{"accounts", "balances", "transactions"}
	}

	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/auth", bytes.NewBuffer(body))
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(httpReq)

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

	var authResp AuthResponse2
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return nil, err
	}

	return &authResp, nil
}

// ========== 4. CREATE SESSION (After redirect back) ==========

type SessionRequest struct {
	Code        string `json:"code"`
	State       string `json:"state"`
}

type SessionResponse struct {
	SessionID string `json:"session_id"`
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

// Create session after successful authorization
func (s *EnableBankingService) CreateSession(ctx context.Context, code, state string) (*SessionResponse, error) {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	payload := SessionRequest{
		Code:  code,
		State: state,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/sessions", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

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

// ========== 5. GET ACCOUNTS ==========

// Get accounts for a session
func (s *EnableBankingService) GetAccounts(ctx context.Context, sessionID string) ([]Account, error) {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", 
		fmt.Sprintf("%s/sessions/%s/accounts", s.BaseURL, sessionID), nil)
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

	var accounts []Account
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		return nil, err
	}

	return accounts, nil
}

// ========== 6. GET BALANCES ==========

type Balance struct {
	AccountID       string  `json:"account_id"`
	BalanceAmount   float64 `json:"balance_amount"`
	BalanceType     string  `json:"balance_type"`
	Currency        string  `json:"currency"`
	ReferenceDate   string  `json:"reference_date"`
}

// Get balances for an account
func (s *EnableBankingService) GetBalances(ctx context.Context, sessionID, accountID string) ([]Balance, error) {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	req, _ := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/sessions/%s/accounts/%s/balances", s.BaseURL, sessionID, accountID), nil)
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

	var balances []Balance
	if err := json.NewDecoder(resp.Body).Decode(&balances); err != nil {
		return nil, err
	}

	return balances, nil
}

// ========== 7. GET TRANSACTIONS ==========

type Transaction struct {
	TransactionID       string  `json:"transaction_id"`
	AccountID           string  `json:"account_id"`
	BookingDate         string  `json:"booking_date"`
	ValueDate           string  `json:"value_date"`
	TransactionAmount   float64 `json:"transaction_amount"`
	Currency            string  `json:"currency"`
	DebtorName          string  `json:"debtor_name"`
	CreditorName        string  `json:"creditor_name"`
	RemittanceInfo      string  `json:"remittance_information"`
}

// Get transactions for an account
func (s *EnableBankingService) GetTransactions(ctx context.Context, sessionID, accountID string, dateFrom, dateTo string) ([]Transaction, error) {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/sessions/%s/accounts/%s/transactions", s.BaseURL, sessionID, accountID)
	if dateFrom != "" && dateTo != "" {
		url += fmt.Sprintf("?date_from=%s&date_to=%s", dateFrom, dateTo)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
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

	var transactions []Transaction
	if err := json.NewDecoder(resp.Body).Decode(&transactions); err != nil {
		return nil, err
	}

	return transactions, nil
}

// ========== 8. DELETE SESSION ==========

// Delete a session (disconnect bank)
func (s *EnableBankingService) DeleteSession(ctx context.Context, sessionID string) error {
	accessToken, err := s.GetAccessToken(ctx)
	if err != nil {
		return err
	}

	req, _ := http.NewRequestWithContext(ctx, "DELETE",
		fmt.Sprintf("%s/sessions/%s", s.BaseURL, sessionID), nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	s.setHeaders(req)

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