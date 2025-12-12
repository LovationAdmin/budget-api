package handlers

import (
    "database/sql"
    "net/http"
    "time"

    "budget-api/middleware"
    "budget-api/services"

    "github.com/gin-gonic/gin"
)

type GoCardlessHandler struct {
    Service       *services.BankingService
    GCService     *services.GoCardlessService
    AccessToken   string // Token de session (cache 24h)
    TokenExpiry   time.Time
}

func NewGoCardlessHandler(db *sql.DB) *GoCardlessHandler {
    return &GoCardlessHandler{
        Service:   services.NewBankingService(db),
        GCService: services.NewGoCardlessService(),
    }
}

// Récupérer un access token (avec cache)
func (h *GoCardlessHandler) getAccessToken(c *gin.Context) (string, error) {
    if h.AccessToken != "" && time.Now().Before(h.TokenExpiry) {
        return h.AccessToken, nil
    }

    token, err := h.GCService.GetAccessToken(c.Request.Context())
    if err != nil {
        return "", err
    }

    h.AccessToken = token
    h.TokenExpiry = time.Now().Add(23 * time.Hour)
    return token, nil
}

// 1. Lister les banques disponibles
func (h *GoCardlessHandler) GetInstitutions(c *gin.Context) {
    token, err := h.getAccessToken(c)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get access token"})
        return
    }

    country := c.DefaultQuery("country", "FR") // Par défaut : France
    institutions, err := h.GCService.GetInstitutions(c.Request.Context(), token, country)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch institutions"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"institutions": institutions})
}

// 2. Créer une connexion bancaire
func (h *GoCardlessHandler) CreateBankConnection(c *gin.Context) {
    userID := middleware.GetUserID(c)

    var req struct {
        InstitutionID string `json:"institution_id" binding:"required"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    token, err := h.getAccessToken(c)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get access token"})
        return
    }

    // URL de retour après connexion
    redirectURL := os.Getenv("FRONTEND_URL") + "/budgets"

    requisitionID, authLink, err := h.GCService.CreateRequisition(
        c.Request.Context(),
        token,
        req.InstitutionID,
        redirectURL,
        userID,
    )

    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create requisition"})
        return
    }

    // Sauvegarder temporairement la requisition (en attendant callback)
    // TODO: Stocker requisitionID en DB avec status "pending"

    c.JSON(http.StatusOK, gin.H{
        "requisition_id": requisitionID,
        "auth_link":      authLink, // Frontend redirige l'utilisateur vers cette URL
    })
}

// 3. Callback après connexion bancaire
func (h *GoCardlessHandler) CompleteConnection(c *gin.Context) {
    userID := middleware.GetUserID(c)
    requisitionID := c.Query("ref") // GoCardless renvoie ?ref=requisition_id

    if requisitionID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing requisition ID"})
        return
    }

    token, err := h.getAccessToken(c)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get access token"})
        return
    }

    // Récupérer la liste des comptes
    accountIDs, err := h.GCService.GetAccounts(c.Request.Context(), token, requisitionID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
        return
    }

    // Pour chaque compte, récupérer détails + solde
    for _, accountID := range accountIDs {
        details, err := h.GCService.GetAccountDetails(c.Request.Context(), token, accountID)
        if err != nil {
            continue
        }

        balance, currency, err := h.GCService.GetAccountBalance(c.Request.Context(), token, accountID)
        if err != nil {
            continue
        }

        // Sauvegarder en DB
        // TODO: Créer une connection avec l'institution_id
        connID := "..." // ID de la connexion créée

        h.Service.SaveAccount(
            c.Request.Context(),
            connID,
            accountID,
            details.Name,
            details.IBAN[len(details.IBAN)-4:], // Derniers 4 chiffres
            currency,
            balance,
        )
    }

    c.JSON(http.StatusOK, gin.H{"message": "Bank connected successfully"})
}