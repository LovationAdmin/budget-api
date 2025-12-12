package services

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "os"
    "time"
)

type BridgeService struct {
    ClientID     string
    ClientSecret string
    BaseURL      string
    Client       *http.Client
    AccessToken  string
    TokenExpiry  time.Time
}

func NewBridgeService() *BridgeService {
    env := os.Getenv("BRIDGE_ENV")
    baseURL := "https://api.bridgeapi.io/v2"
    
    if env == "sandbox" {
        baseURL = "https://api.bridgeapi.io/v2" // Même URL, différence dans les credentials
    }

    return &BridgeService{
        ClientID:     os.Getenv("BRIDGE_CLIENT_ID"),
        ClientSecret: os.Getenv("BRIDGE_CLIENT_SECRET"),
        BaseURL:      baseURL,
        Client:       &http.Client{Timeout: 30 * time.Second},
    }
}

// 1. Authentification (JWT Token)
func (s *BridgeService) GetAccessToken(ctx context.Context) (string, error) {
    // Cache le token s'il est encore valide
    if s.AccessToken != "" && time.Now().Before(s.TokenExpiry) {
        return s.AccessToken, nil
    }

    payload := map[string]string{
        "client_id":     s.ClientID,
        "client_secret": s.ClientSecret,
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/authenticate", bytes.NewBuffer(body))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Bridge-Version", "2021-06-01")

    resp, err := s.Client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        respBody, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("auth failed: %s", string(respBody))
    }

    var result struct {
        AccessToken string `json:"access_token"`
        ExpiresAt   string `json:"expires_at"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    s.AccessToken = result.AccessToken
    // Token valide 30 min, on garde une marge de 5 min
    s.TokenExpiry = time.Now().Add(25 * time.Minute)

    return s.AccessToken, nil
}

// 2. Créer un "Connect Item" (= connexion bancaire)
func (s *BridgeService) CreateConnectItem(ctx context.Context, accessToken, prefillEmail string) (string, error) {
    payload := map[string]interface{}{
        "prefill_email": prefillEmail, // Email de l'utilisateur (optionnel)
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/connect/items/add", bytes.NewBuffer(body))
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Bridge-Version", "2021-06-01")

    resp, err := s.Client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        respBody, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("create item failed: %s", string(respBody))
    }

    var result struct {
        RedirectURL string `json:"redirect_url"` // URL où rediriger l'utilisateur
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    return result.RedirectURL, nil
}

// 3. Récupérer les comptes bancaires d'un utilisateur
type BridgeAccount struct {
    ID       int64   `json:"id"`
    Name     string  `json:"name"`
    Balance  float64 `json:"balance"`
    Currency string  `json:"currency_code"`
    IBAN     string  `json:"iban"`
    Type     string  `json:"type"`
}

func (s *BridgeService) GetAccounts(ctx context.Context, accessToken string, userID string) ([]BridgeAccount, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/accounts?user_id="+userID, nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Bridge-Version", "2021-06-01")

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

// 4. Récupérer les transactions d'un compte
type BridgeTransaction struct {
    ID          int64     `json:"id"`
    Description string    `json:"description"`
    Amount      float64   `json:"amount"`
    Date        time.Time `json:"date"`
    Category    string    `json:"category_id"`
}

func (s *BridgeService) GetTransactions(ctx context.Context, accessToken string, accountID int64, since time.Time) ([]BridgeTransaction, error) {
    url := fmt.Sprintf("%s/accounts/%d/transactions?since=%s", s.BaseURL, accountID, since.Format("2006-01-02"))
    
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Bridge-Version", "2021-06-01")

    resp, err := s.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Resources []BridgeTransaction `json:"resources"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    return result.Resources, nil
}

// 5. Lister les banques disponibles
type BridgeBank struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
    Logo string `json:"logo_url"`
}

func (s *BridgeService) GetBanks(ctx context.Context, accessToken string) ([]BridgeBank, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/banks", nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Bridge-Version", "2021-06-01")

    resp, err := s.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Resources []BridgeBank `json:"resources"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    return result.Resources, nil
}

// 6. Rafraîchir les données d'un compte
func (s *BridgeService) RefreshAccount(ctx context.Context, accessToken string, accountID int64) error {
    req, _ := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/accounts/%d/refresh", s.BaseURL, accountID), nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Bridge-Version", "2021-06-01")

    resp, err := s.Client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 && resp.StatusCode != 202 {
        respBody, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("refresh failed: %s", string(respBody))
    }

    return nil
}