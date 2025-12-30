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
		budgetID, exists := s.Get("budget_id")
		if exists {
			log.Printf("✅ Client connected to budget: %s", budgetID)
		} else {
			log.Printf("⚠️ Client connected but no budget_id set")
		}
	})

	m.HandleDisconnect(func(s *melody.Session) {
		budgetID, _ := s.Get("budget_id")
		log.Printf("🔌 Client disconnected from budget: %v", budgetID)
	})

	m.HandleError(func(s *melody.Session, err error) {
		log.Printf("⚠️ WebSocket Error: %v", err)
	})

	return &WSHandler{M: m}
}

func (h *WSHandler) HandleWS(c *gin.Context) {
	budgetID := c.Param("id")
	
	log.Printf("🔌 Incoming WS connection request for budget: %s", budgetID)

	// 🔥 FIX: Set budget_id AVANT l'upgrade
	c.Set("budget_id", budgetID)

	// Upgrade request to WebSocket
	err := h.M.HandleRequestWithKeys(c.Writer, c.Request, map[string]interface{}{
		"budget_id": budgetID,
	})
	
	if err != nil {
		log.Printf("❌ Failed to upgrade websocket: %v", err)
		return
	}
}

// BroadcastUpdate sends a simple update signal
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