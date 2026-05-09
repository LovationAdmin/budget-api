// handlers/devices.go
// ============================================================================
// USER DEVICES (Expo push tokens)
// ============================================================================
// Per-install Expo push tokens. The mobile app calls POST /user/devices on
// every login (idempotent — uniqueness on expo_push_token). The server uses
// the table to fan-out push notifications via Expo Push Service.
// ============================================================================

package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/utils"
)

type DevicesHandler struct {
	DB *sql.DB
}

func NewDevicesHandler(db *sql.DB) *DevicesHandler {
	return &DevicesHandler{DB: db}
}

type registerDeviceRequest struct {
	ExpoPushToken string `json:"expo_push_token" binding:"required"`
	Platform      string `json:"platform"        binding:"required,oneof=ios android"`
	AppVersion    string `json:"app_version"`
}

// Register — POST /api/v1/user/devices
// Idempotent upsert keyed by expo_push_token. Updates last_seen_at on each call.
func (h *DevicesHandler) Register(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req registerDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id := uuid.New().String()
	_, err := h.DB.Exec(`
		INSERT INTO user_devices (id, user_id, expo_push_token, platform, app_version, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NOW(), NOW())
		ON CONFLICT (expo_push_token) DO UPDATE
		SET user_id      = EXCLUDED.user_id,
		    platform     = EXCLUDED.platform,
		    app_version  = COALESCE(EXCLUDED.app_version, user_devices.app_version),
		    last_seen_at = NOW()
	`, id, userID, req.ExpoPushToken, req.Platform, req.AppVersion)
	if err != nil {
		utils.SafeError("devices register: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register device"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Device registered"})
}

// List — GET /api/v1/user/devices
func (h *DevicesHandler) List(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, expo_push_token, platform, COALESCE(app_version, ''),
		       created_at, last_seen_at
		FROM user_devices WHERE user_id = $1
		ORDER BY last_seen_at DESC
	`, userID)
	if err != nil {
		utils.SafeError("devices list: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list devices"})
		return
	}
	defer rows.Close()

	out := []gin.H{}
	for rows.Next() {
		var (
			id, tok, plat, ver string
			created, seen      string
		)
		if err := rows.Scan(&id, &tok, &plat, &ver, &created, &seen); err != nil {
			continue
		}
		out = append(out, gin.H{
			"id":              id,
			"expo_push_token": tok,
			"platform":        plat,
			"app_version":     ver,
			"created_at":      created,
			"last_seen_at":    seen,
		})
	}

	c.JSON(http.StatusOK, out)
}

// Delete — DELETE /api/v1/user/devices  (body: { expo_push_token })
// Used when the app uninstalls / logs out. We delete by token, not ID, so the
// client doesn't need to track the server-side row id.
func (h *DevicesHandler) Delete(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		ExpoPushToken string `json:"expo_push_token"`
	}
	_ = c.ShouldBindJSON(&req)

	if req.ExpoPushToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expo_push_token required"})
		return
	}

	if _, err := h.DB.Exec(`
		DELETE FROM user_devices WHERE user_id = $1 AND expo_push_token = $2
	`, userID, req.ExpoPushToken); err != nil {
		utils.SafeError("devices delete: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete device"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device removed"})
}
