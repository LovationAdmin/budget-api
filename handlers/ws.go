package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/olahol/melody"
	"log"
)

type WSHandler struct {
	M *melody.Melody
}

func NewWSHandler() *WSHandler {
	m := melody.New()
	
	// Configurer la taille max des messages
	m.Config.MaxMessageSize = 1024 * 1024 

	return &WSHandler{M: m}
}

// HandleWS gère la connexion WebSocket
func (h *WSHandler) HandleWS(c *gin.Context) {
	budgetID := c.Param("id")
	
	// On upgrade la requête HTTP en WebSocket
	// On passe le budgetID dans le contexte de la session Melody
	h.M.HandleRequest(c.Writer, c.Request)
	
	h.M.HandleConnect(func(s *melody.Session) {
		// On stocke sur quel budget l'utilisateur est connecté
		// Note: Dans une vraie app, on vérifierait le Token JWT ici aussi
		s.Set("budget_id", budgetID)
		log.Printf("Client connecté au budget: %s", budgetID)
	})
}

// BroadcastUpdate envoie un signal à tous les clients écoutant ce budget
func (h *WSHandler) BroadcastUpdate(budgetID string, updateType string, userWhoUpdated string) {
	msg := []byte(fmt.Sprintf(`{"type": "%s", "user": "%s"}`, updateType, userWhoUpdated))
	
	h.M.BroadcastFilter(msg, func(q *melody.Session) bool {
		// On n'envoie qu'aux gens connectés sur CE budget
		id, exists := q.Get("budget_id")
		return exists && id == budgetID
	})
}