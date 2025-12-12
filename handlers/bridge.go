package handlers

import (
    "database/sql"
    "net/http"
    "strconv"

    "budget-api/middleware"
    "budget-api/services"

    "github.com/gin-gonic/gin"
)

type BridgeHandler struct {
    DB            *sql.DB 
    Service       *services.BankingService
    BridgeService *services.BridgeService
}

func NewBridgeHandler(db *sql.DB) *BridgeHandler {
    return &BridgeHandler{
        DB:            db,
        Service:       services.NewBankingService(db),
        BridgeService: services.NewBridgeService(),
    }
}

// 1. Lister les banques disponibles
func (h *BridgeHandler) GetBanks(c *gin.Context) {
    token, err := h.BridgeService.GetAccessToken(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate"})
        return
    }

    banks, err := h.BridgeService.GetBanks(c.Request.Context(), token)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch banks"})
        return
    }

    c.JSON(http.StatusOK, gin.H{"banks": banks})
}

// 2. Créer une connexion bancaire
func (h *BridgeHandler) CreateConnection(c *gin.Context) {
    userID := middleware.GetUserID(c)

    token, err := h.BridgeService.GetAccessToken(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate"})
        return
    }

    // Récupérer l'email de l'utilisateur (optionnel, pour préfill)
    var user struct {
        Email string
    }
    h.DB.QueryRow("SELECT email FROM users WHERE id = $1", userID).Scan(&user.Email)

    redirectURL, err := h.BridgeService.CreateConnectItem(c.Request.Context(), token, user.Email)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create connection"})
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "redirect_url": redirectURL, // Frontend redirige vers cette URL
    })
}

// 3. Callback après connexion (Bridge webhook ou redirect)
func (h *BridgeHandler) HandleCallback(c *gin.Context) {
    userID := middleware.GetUserID(c)
    itemID := c.Query("item_id") // Bridge renvoie item_id après connexion

    if itemID == "" {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Missing item_id"})
        return
    }

    token, err := h.BridgeService.GetAccessToken(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate"})
        return
    }

    // Récupérer les comptes de cet item
    accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), token, userID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
        return
    }

    // Sauvegarder les comptes en DB
    for _, acc := range accounts {
        // TODO: Créer une connection en DB si nécessaire
        connID := "..." // ID de la connexion

        h.Service.SaveAccount(
            c.Request.Context(),
            connID,
            strconv.FormatInt(acc.ID, 10),
            acc.Name,
            acc.IBAN[len(acc.IBAN)-4:], // 4 derniers chiffres
            acc.Currency,
            acc.Balance,
        )
    }

    c.JSON(http.StatusOK, gin.H{"message": "Bank connected successfully"})
}

// 4. Rafraîchir les soldes
func (h *BridgeHandler) RefreshBalances(c *gin.Context) {
    userID := middleware.GetUserID(c)

    token, err := h.BridgeService.GetAccessToken(c.Request.Context())
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to authenticate"})
        return
    }

    // Récupérer tous les comptes de l'utilisateur
    accounts, err := h.BridgeService.GetAccounts(c.Request.Context(), token, userID)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch accounts"})
        return
    }

    // Mettre à jour les soldes en DB
    for _, acc := range accounts {
        // TODO: Update balance in DB
        _ = acc
    }

    c.JSON(http.StatusOK, gin.H{"message": "Balances refreshed"})
}