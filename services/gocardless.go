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

type GoCardlessService struct {
    SecretID  string
    SecretKey string
    BaseURL   string
    Client    *http.Client
}

func NewGoCardlessService() *GoCardlessService {
    return &GoCardlessService{
        SecretID:  os.Getenv("GOCARDLESS_SECRET_ID"),
        SecretKey: os.Getenv("GOCARDLESS_SECRET_KEY"),
        BaseURL:   "https://bankaccountdata.gocardless.com/api/v2",
        Client:    &http.Client{Timeout: 30 * time.Second},
    }
}

// 1. Obtenir un token d'accès (valide 24h)
func (s *GoCardlessService) GetAccessToken(ctx context.Context) (string, error) {
    payload := map[string]string{
        "secret_id":  s.SecretID,
        "secret_key": s.SecretKey,
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/token/new/", bytes.NewBuffer(body))
    req.Header.Set("Content-Type", "application/json")

    resp, err := s.Client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    var result struct {
        Access       string `json:"access"`
        AccessExpires int   `json:"access_expires"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return "", err
    }

    return result.Access, nil
}

// 2. Créer une "requisition" (= demande de connexion bancaire)
func (s *GoCardlessService) CreateRequisition(ctx context.Context, accessToken, institutionID, redirectURL, userID string) (string, string, error) {
    payload := map[string]string{
        "redirect":      redirectURL,
        "institution_id": institutionID,
        "reference":     userID, // Pour retrouver l'utilisateur
    }

    body, _ := json.Marshal(payload)
    req, _ := http.NewRequestWithContext(ctx, "POST", s.BaseURL+"/requisitions/", bytes.NewBuffer(body))
    req.Header.Set("Authorization", "Bearer "+accessToken)
    req.Header.Set("Content-Type", "application/json")

    resp, err := s.Client.Do(req)
    if err != nil {
        return "", "", err
    }
    defer resp.Body.Close()

    respBody, _ := io.ReadAll(resp.Body)

    var result struct {
        ID   string `json:"id"`
        Link string `json:"link"` // URL où rediriger l'utilisateur
    }

    if err := json.Unmarshal(respBody, &result); err != nil {
        return "", "", fmt.Errorf("decode error: %v, body: %s", err, string(respBody))
    }

    return result.ID, result.Link, nil
}

// 3. Récupérer les comptes bancaires après connexion
func (s *GoCardlessService) GetAccounts(ctx context.Context, accessToken, requisitionID string) ([]string, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/requisitions/"+requisitionID+"/", nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)

    resp, err := s.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Accounts []string `json:"accounts"` // Liste d'account IDs
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    return result.Accounts, nil
}

// 4. Récupérer les détails d'un compte (nom, IBAN, etc.)
type AccountDetails struct {
    IBAN     string  `json:"iban"`
    Name     string  `json:"name"`
    Currency string  `json:"currency"`
}

func (s *GoCardlessService) GetAccountDetails(ctx context.Context, accessToken, accountID string) (*AccountDetails, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/accounts/"+accountID+"/details/", nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)

    resp, err := s.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result struct {
        Account AccountDetails `json:"account"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    return &result.Account, nil
}

// 5. Récupérer le solde d'un compte
type AccountBalance struct {
    Amount   string `json:"balanceAmount.amount"`
    Currency string `json:"balanceAmount.currency"`
}

func (s *GoCardlessService) GetAccountBalance(ctx context.Context, accessToken, accountID string) (float64, string, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/accounts/"+accountID+"/balances/", nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)

    resp, err := s.Client.Do(req)
    if err != nil {
        return 0, "", err
    }
    defer resp.Body.Close()

    var result struct {
        Balances []struct {
            BalanceAmount struct {
                Amount   string `json:"amount"`
                Currency string `json:"currency"`
            } `json:"balanceAmount"`
            BalanceType string `json:"balanceType"`
        } `json:"balances"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return 0, "", err
    }

    // Chercher le solde "interimAvailable" ou "expected"
    for _, bal := range result.Balances {
        if bal.BalanceType == "interimAvailable" || bal.BalanceType == "expected" {
            var amount float64
            fmt.Sscanf(bal.BalanceAmount.Amount, "%f", &amount)
            return amount, bal.BalanceAmount.Currency, nil
        }
    }

    return 0, "", fmt.Errorf("no suitable balance found")
}

// 6. Lister les banques disponibles par pays
type Institution struct {
    ID   string `json:"id"`
    Name string `json:"name"`
    Logo string `json:"logo"`
}

func (s *GoCardlessService) GetInstitutions(ctx context.Context, accessToken, countryCode string) ([]Institution, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/institutions/?country="+countryCode, nil)
    req.Header.Set("Authorization", "Bearer "+accessToken)

    resp, err := s.Client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var institutions []Institution
    if err := json.NewDecoder(resp.Body).Decode(&institutions); err != nil {
        return nil, err
    }

    return institutions, nil
}