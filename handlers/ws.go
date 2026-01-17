// handlers/ws.go
// ============================================================================
// WEBSOCKET HANDLER - Communication temps réel pour synchronisation budgets
// ============================================================================
// VERSION CORRIGÉE : Logging sécurisé
// ============================================================================

package handlers

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/LovationAdmin/budget-api/utils"
)

// ============================================================================
// TYPES & CONFIGURATION
// ============================================================================

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		allowedOrigins := []string{
			"https://budgetfamille.com",
			"https://www.budgetfamille.com",
			"https://budget-ui-two.vercel.app",
			"http://localhost:3000",
			"http://localhost:5173",
		}
		for _, allowed := range allowedOrigins {
			if origin == allowed {
				return true
			}
		}
		return false
	},
}

type WSSession struct {
	Conn     *websocket.Conn
	BudgetID string
	UserID   string
}

type WSHandler struct {
	sessions map[string]*WSSession // key = conn pointer address
	mu       sync.RWMutex
}

// ============================================================================
// CONSTRUCTOR
// ============================================================================

func NewWSHandler() *WSHandler {
	return &WSHandler{
		sessions: make(map[string]*WSSession),
	}
}

// ============================================================================
// WEBSOCKET CONNECTION HANDLER
// ============================================================================

func (h *WSHandler) HandleWS(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.Query("user_id")

	if budgetID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Budget ID required"})
		return
	}

	// ✅ LOGGING SÉCURISÉ
	utils.LogWebSocket("Connect", budgetID, userID)

	// Upgrade HTTP to WebSocket
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		utils.SafeError("WebSocket upgrade failed: %v", err)
		return
	}

	// Create session
	sessionKey := conn.RemoteAddr().String()
	session := &WSSession{
		Conn:     conn,
		BudgetID: budgetID,
		UserID:   userID,
	}

	// Register session
	h.mu.Lock()
	h.sessions[sessionKey] = session
	h.mu.Unlock()

	utils.SafeInfo("WebSocket client connected (budget sessions: %d)", h.countBudgetSessions(budgetID))

	// Handle messages
	go h.handleMessages(sessionKey, session)
}

// ============================================================================
// MESSAGE HANDLING
// ============================================================================

func (h *WSHandler) handleMessages(sessionKey string, session *WSSession) {
	defer func() {
		// Cleanup on disconnect
		h.mu.Lock()
		delete(h.sessions, sessionKey)
		h.mu.Unlock()

		session.Conn.Close()
		utils.LogWebSocket("Disconnect", session.BudgetID, session.UserID)
	}()

	for {
		_, msg, err := session.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.SafeWarn("WebSocket read error: %v", err)
			}
			break
		}

		// Parse message
		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}

		// Handle ping/pong for keepalive
		if msgType, ok := data["type"].(string); ok {
			if msgType == "ping" {
				session.Conn.WriteJSON(map[string]string{"type": "pong"})
			}
		}
	}
}

// ============================================================================
// BROADCAST METHODS
// ============================================================================

// BroadcastJSON envoie un message JSON à tous les clients d'un budget
func (h *WSHandler) BroadcastJSON(budgetID string, payload interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sentCount := 0
	for _, session := range h.sessions {
		if session.BudgetID == budgetID {
			if err := session.Conn.WriteJSON(payload); err != nil {
				utils.SafeWarn("Failed to send WebSocket message: %v", err)
				continue
			}
			sentCount++
		}
	}

	if sentCount > 0 {
		utils.SafeInfo("Broadcast sent to %d clients", sentCount)
	}
}

// BroadcastUpdateExcludingUser envoie un message à tous sauf l'utilisateur spécifié
func (h *WSHandler) BroadcastUpdateExcludingUser(budgetID string, excludeUserID string, updateType string, userName string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	payload := map[string]interface{}{
		"type": updateType,
		"user": userName,
	}

	sentCount := 0
	for _, session := range h.sessions {
		if session.BudgetID == budgetID && session.UserID != excludeUserID {
			if err := session.Conn.WriteJSON(payload); err != nil {
				utils.SafeWarn("Failed to send WebSocket message: %v", err)
				continue
			}
			sentCount++
		}
	}

	if sentCount > 0 {
		utils.SafeInfo("Update broadcast to %d clients (excluding modifier)", sentCount)
	}
}

// BroadcastToUser envoie un message à un utilisateur spécifique
func (h *WSHandler) BroadcastToUser(budgetID string, userID string, payload interface{}) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, session := range h.sessions {
		if session.BudgetID == budgetID && session.UserID == userID {
			if err := session.Conn.WriteJSON(payload); err != nil {
				utils.SafeWarn("Failed to send WebSocket message to user: %v", err)
			}
			return
		}
	}
}

// ============================================================================
// UTILITY METHODS
// ============================================================================

// countBudgetSessions compte le nombre de sessions pour un budget
func (h *WSHandler) countBudgetSessions(budgetID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, session := range h.sessions {
		if session.BudgetID == budgetID {
			count++
		}
	}
	return count
}

// GetConnectedUsers retourne la liste des utilisateurs connectés à un budget
func (h *WSHandler) GetConnectedUsers(budgetID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := make([]string, 0)
	seen := make(map[string]bool)

	for _, session := range h.sessions {
		if session.BudgetID == budgetID && session.UserID != "" {
			if !seen[session.UserID] {
				users = append(users, session.UserID)
				seen[session.UserID] = true
			}
		}
	}

	return users
}

// IsUserConnected vérifie si un utilisateur est connecté à un budget
func (h *WSHandler) IsUserConnected(budgetID string, userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, session := range h.sessions {
		if session.BudgetID == budgetID && session.UserID == userID {
			return true
		}
	}
	return false
}

// GetStats retourne les statistiques globales des connexions WebSocket
func (h *WSHandler) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	budgets := make(map[string]int)
	for _, session := range h.sessions {
		budgets[session.BudgetID]++
	}

	return map[string]interface{}{
		"total_connections": len(h.sessions),
		"active_budgets":    len(budgets),
	}
}