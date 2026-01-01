package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/olahol/melody"
)

type WSHandler struct {
	M *melody.Melody
}

func NewWSHandler() *WSHandler {
	m := melody.New()
	
	m.Config.MaxMessageSize = 1024 * 1024 
	
	// Keep-Alive Configuration
	m.Config.PingPeriod = 30 * time.Second
	m.Config.PongWait = 60 * time.Second

	// Allow Cross-Origin WebSockets
	m.Upgrader.CheckOrigin = func(r *http.Request) bool {
		return true
	}

	// 🔥 FIX: HandleConnect doit être enregistré UNE SEULE FOIS ici
	m.HandleConnect(func(s *melody.Session) {
		budgetID, budgetExists := s.Get("budget_id")
		userID, userExists := s.Get("user_id")
		
		if budgetExists && userExists {
			log.Printf("✅ Client connected to budget: %s (user: %s)", budgetID, userID)
		} else if budgetExists {
			log.Printf("⚠️ Client connected to budget: %s (no user_id)", budgetID)
		} else {
			log.Printf("⚠️ Client connected but no budget_id/user_id set")
		}
	})

	m.HandleDisconnect(func(s *melody.Session) {
		budgetID, _ := s.Get("budget_id")
		userID, _ := s.Get("user_id")
		log.Printf("🔌 Client disconnected from budget: %v (user: %v)", budgetID, userID)
	})

	m.HandleError(func(s *melody.Session, err error) {
		log.Printf("⚠️ WebSocket Error: %v", err)
	})

	return &WSHandler{M: m}
}

func (h *WSHandler) HandleWS(c *gin.Context) {
	budgetID := c.Param("id")
	userID := c.GetString("user_id") // 🔥 NEW: Get authenticated user ID
	
	log.Printf("🔌 Incoming WS connection request for budget: %s from user: %s", budgetID, userID)

	// 🔥 FIX: Set budget_id AND user_id AVANT l'upgrade
	c.Set("budget_id", budgetID)
	c.Set("user_id", userID)

	// Upgrade request to WebSocket
	err := h.M.HandleRequestWithKeys(c.Writer, c.Request, map[string]interface{}{
		"budget_id": budgetID,
		"user_id":   userID, // 🔥 NEW: Store user_id in session
	})
	
	if err != nil {
		log.Printf("❌ Failed to upgrade websocket: %v", err)
		return
	}
}

// BroadcastUpdate sends a simple update signal (LEGACY - kept for compatibility)
func (h *WSHandler) BroadcastUpdate(budgetID string, updateType string, userWhoUpdated string) {
	msg := []byte(`{"type": "` + updateType + `", "user": "` + userWhoUpdated + `"}`)
	
	err := h.M.BroadcastFilter(msg, func(q *melody.Session) bool {
		id, exists := q.Get("budget_id")
		return exists && id == budgetID
	})

	if err != nil {
		log.Printf("⚠️ Error broadcasting to budget %s: %v", budgetID, err)
	}
}

// BroadcastUpdateExcludingUser sends update to all users EXCEPT the one who made the change
// 🔥 NEW: This is the preferred method for budget updates
func (h *WSHandler) BroadcastUpdateExcludingUser(budgetID string, updateType string, userWhoUpdated string, userIDToExclude string) {
	msg := []byte(`{"type": "` + updateType + `", "user": "` + userWhoUpdated + `", "user_id": "` + userIDToExclude + `"}`)
	
	err := h.M.BroadcastFilter(msg, func(q *melody.Session) bool {
		id, exists := q.Get("budget_id")
		if !exists || id != budgetID {
			return false
		}
		
		// 🔥 EXCLUDE the user who made the update
		excludeUserID, hasExclude := q.Get("user_id")
		if hasExclude && excludeUserID == userIDToExclude {
			log.Printf("🚫 Skipping notification for user who made the update: %s", userIDToExclude)
			return false
		}
		
		return true
	})

	if err != nil {
		log.Printf("⚠️ Error broadcasting to budget %s: %v", budgetID, err)
	} else {
		log.Printf("📢 Broadcasted update to budget %s (excluding user %s)", budgetID, userIDToExclude)
	}
}

// BroadcastJSON sends any struct as JSON payload
func (h *WSHandler) BroadcastJSON(budgetID string, payload interface{}) {
	msg, err := json.Marshal(payload)
	if err != nil {
		log.Printf("❌ Failed to marshal JSON for broadcast: %v", err)
		return
	}

	err = h.M.BroadcastFilter(msg, func(q *melody.Session) bool {
		id, exists := q.Get("budget_id")
		return exists && id == budgetID
	})

	if err != nil {
		log.Printf("⚠️ Error broadcasting JSON to budget %s: %v", budgetID, err)
	}
}