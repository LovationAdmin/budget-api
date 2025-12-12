package services

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/plaid/plaid-go/v20/plaid"
)

type PlaidService struct {
	Client *plaid.APIClient
}

func NewPlaidService() *PlaidService {
	clientID := os.Getenv("PLAID_CLIENT_ID")
	secret := os.Getenv("PLAID_SECRET")
	envStr := os.Getenv("PLAID_ENV")

	var env plaid.Environment
	switch envStr {
	case "production":
		env = plaid.Production
	case "development":
		env = plaid.Development
	default:
		env = plaid.Sandbox
	}

	configuration := plaid.NewConfiguration()
	configuration.AddDefaultHeader("PLAID-CLIENT-ID", clientID)
	configuration.AddDefaultHeader("PLAID-SECRET", secret)
	configuration.UseEnvironment(env)

	return &PlaidService{
		Client: plaid.NewAPIClient(configuration),
	}
}

// 1. Create Link Token (Frontend uses this to open the widget)
func (s *PlaidService) CreateLinkToken(ctx context.Context, userID string) (string, error) {
	user := plaid.LinkTokenCreateRequestUser{
		ClientUserId: userID,
	}

	// 1. Get Base URL from Environment
	frontendURL := os.Getenv("FRONTEND_URL")
	
	// DEBUG LOG
	fmt.Printf("[DEBUG] Raw FRONTEND_URL env var: '%s'\n", frontendURL)

	if frontendURL == "" {
		frontendURL = "http://localhost:3000" // Default for local dev
	}

	// 2. Ensure consistency: Always append a trailing slash
	redirectURI := frontendURL
	if !strings.HasSuffix(redirectURI, "/") {
		redirectURI = redirectURI + "/"
	}

	// DEBUG LOG
	fmt.Printf("[DEBUG] Final Redirect URI sent to Plaid: '%s'\n", redirectURI)

	// --- SANITY CHECK CONFIGURATION (US ONLY) ---
	// We use "en" and "US" because these are guaranteed to be enabled in all Sandbox accounts.
	// This rules out "Country Not Enabled" errors.
	request := plaid.NewLinkTokenCreateRequest(
		"Budget Famille",
		"en", 
		[]plaid.CountryCode{plaid.COUNTRYCODE_US}, 
		user,
	)

	// We specifically ask for Transactions/Balance permissions
	request.SetProducts([]plaid.Products{plaid.PRODUCTS_TRANSACTIONS})

	// 3. Set the Dynamic Redirect URI
	request.SetRedirectUri(redirectURI)

	resp, _, err := s.Client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
	if err != nil {
		fmt.Printf("[ERROR] Plaid CreateLinkToken Failed: %v\n", formatPlaidError(err))
		return "", formatPlaidError(err)
	}

	return resp.GetLinkToken(), nil
}

// 2. Exchange Public Token (Frontend sends this) for Access Token (Backend saves this)
func (s *PlaidService) ExchangePublicToken(ctx context.Context, publicToken string) (string, string, error) {
	request := plaid.NewItemPublicTokenExchangeRequest(publicToken)

	resp, _, err := s.Client.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(*request).Execute()
	if err != nil {
		return "", "", formatPlaidError(err)
	}

	return resp.GetAccessToken(), resp.GetItemId(), nil
}

// 3. Fetch Real Balances
func (s *PlaidService) GetBalances(ctx context.Context, accessToken string) ([]plaid.AccountBase, error) {
	request := plaid.NewAccountsBalanceGetRequest(accessToken)

	resp, _, err := s.Client.PlaidApi.AccountsBalanceGet(ctx).AccountsBalanceGetRequest(*request).Execute()
	if err != nil {
		return nil, formatPlaidError(err)
	}

	return resp.GetAccounts(), nil
}

// Helper for error formatting
func formatPlaidError(err error) error {
	if plaidErr, ok := err.(plaid.GenericOpenAPIError); ok {
		// Try to read the body for more details
		return fmt.Errorf("plaid error: %s", string(plaidErr.Body()))
	}
	return err
}