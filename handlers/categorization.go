package handlers

import (
	"database/sql"
	"net/http"

	"github.com/LovationAdmin/budget-api/services"

	"github.com/gin-gonic/gin"
)

type CategorizationHandler struct {
	Service *services.CategorizerService
}

func NewCategorizationHandler(db *sql.DB) *CategorizationHandler {
	return &CategorizationHandler{
		Service: services.NewCategorizerService(db),
	}
}

type CategorizeRequest struct {
	Label string `json:"label" binding:"required"`
}

func (h *CategorizationHandler) CategorizeLabel(c *gin.Context) {
	var req CategorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	category, err := h.Service.GetCategory(c.Request.Context(), req.Label)
	if err != nil {
		// Fallback silencieux en cas d'erreur grave
		c.JSON(http.StatusOK, gin.H{"category": "OTHER", "source": "fallback"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"label":    req.Label,
		"category": category,
	})
}