package handlers

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/olahol/melody"
)

type WSHandler struct {
	M *melody.Melody
}

func NewWSHandler() *WSHandler {
	m := melody.New()
	
	// Configurer la taille max des messages
	m.Config.MaxMessageSize = 1024 * 1024 
	
	// Keep-Alive Configuration (Critical for Render.com/Cloud hosting)
	m.Config.PingPeriod = 30 * time.Second
	m.Config.PongWait = 60 * time.Second

	// Handle disconnects for logging
	m.HandleDisconnect(func(s *melody.Session) {
		budgetID, _ := s.Get("budget_id")
		log.Printf("🔌 Client disconnected from budget: %v", budgetID)
	})

	m.HandleError(func(s *melody.Session, err error) {
		log.Printf("❌ WebSocket Error: %v", err)
	})

	return &WSHandler{M: m}
}

// HandleWS gère la connexion WebSocket
func (h *WSHandler) HandleWS(c *gin.Context) {
	budgetID := c.Param("id")
	
	// Upgrade request to WebSocket
	err := h.M.HandleRequest(c.Writer, c.Request)
	if err != nil {
		log.Printf("❌ Failed to upgrade websocket: %v", err)
		return
	}
	
	h.M.HandleConnect(func(s *melody.Session) {
		s.Set("budget_id", budgetID)
		log.Printf("✅ Client connected to budget: %s", budgetID)
	})
}

// BroadcastUpdate envoie un signal à tous les clients écoutant ce budget
func (h *WSHandler) BroadcastUpdate(budgetID string, updateType string, userWhoUpdated string) {
	// Simple JSON construction to avoid struct overhead for simple signals
	msg := []byte(`{"type": "` + updateType + `", "user": "` + userWhoUpdated + `"}`)
	
	err := h.M.BroadcastFilter(msg, func(q *melody.Session) bool {
		id, exists := q.Get("budget_id")
		return exists && id == budgetID
	})

	if err != nil {
		log.Printf("⚠️ Error broadcasting to budget %s: %v", budgetID, err)
	}
}