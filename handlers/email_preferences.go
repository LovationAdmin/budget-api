// handlers/email_preferences.go
// ============================================================================
// EMAIL PREFERENCES — leave (or come back to) the monthly recap and the news
// ============================================================================
// Public, signed by the token of the email (utils/email_unsubscribe.go):
//   POST /api/v1/email/unsubscribe?token=…   one click (RFC 8058: mail
//        clients POST "List-Unsubscribe=One-Click"), and the app's page
//   POST /api/v1/email/resubscribe?token=…   the page's "I changed my mind"
//   GET  /api/v1/email/preferences?token=…   what the page shows
// From the account page (signed in):
//   PUT  /api/v1/user/email-preferences      {"monthly_recap": bool, "news": bool}
// ============================================================================

package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LovationAdmin/budget-api/middleware"
	"github.com/LovationAdmin/budget-api/utils"
)

type EmailPreferencesHandler struct {
	DB *sql.DB
}

// emailListColumn is the users column of each list (never user input).
var emailListColumn = map[string]string{
	utils.EmailListMonthlyRecap: "email_monthly_recap",
	utils.EmailListNews:         "email_news",
}

type emailListState struct {
	List       string `json:"list"`
	Subscribed bool   `json:"subscribed"`
	// Email is masked: the page only reminds whose address it is.
	Email string `json:"email"`
}

func emailToken(c *gin.Context) string {
	if t := c.Query("token"); t != "" {
		return t
	}
	var body struct {
		Token string `json:"token"`
	}
	if c.ContentType() == "application/json" && c.ShouldBindJSON(&body) == nil {
		return body.Token
	}
	return ""
}

// setFromToken subscribes or unsubscribes the token's user from its list.
func (h *EmailPreferencesHandler) setFromToken(c *gin.Context, subscribed bool) {
	userID, list, ok := utils.ParseUnsubscribeToken(emailToken(c))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link"})
		return
	}
	var email string
	err := h.DB.QueryRow(`UPDATE users SET `+emailListColumn[list]+` = $1, updated_at = NOW() WHERE id = $2 RETURNING email`,
		subscribed, userID).Scan(&email)
	if err == sql.ErrNoRows {
		// The account is gone: nothing will be sent to it any more.
		c.JSON(http.StatusOK, emailListState{List: list, Subscribed: false})
		return
	}
	if err != nil {
		utils.SafeError("email-preferences: update %s: %v", list, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}
	utils.SafeInfo("email-preferences: %s subscribed=%v for %s", list, subscribed, utils.MaskEmailForLog(email))
	c.JSON(http.StatusOK, emailListState{List: list, Subscribed: subscribed, Email: utils.MaskEmailForLog(email)})
}

// Unsubscribe — POST /api/v1/email/unsubscribe?token=…
func (h *EmailPreferencesHandler) Unsubscribe(c *gin.Context) { h.setFromToken(c, false) }

// OpenUnsubscribe — GET /api/v1/email/unsubscribe?token=…: a mail client that
// opens the List-Unsubscribe link in a browser lands on the app's page, which
// unsubscribes and offers to come back. A GET never changes anything itself
// (link scanners follow links).
func (h *EmailPreferencesHandler) OpenUnsubscribe(c *gin.Context) {
	userID, list, ok := utils.ParseUnsubscribeToken(c.Query("token"))
	if !ok {
		c.Redirect(http.StatusFound, frontendURLOrDefault()+"/emails/desabonnement")
		return
	}
	c.Redirect(http.StatusFound, utils.UnsubscribeLinksFor(userID, list).Page)
}

// Resubscribe — POST /api/v1/email/resubscribe?token=…
func (h *EmailPreferencesHandler) Resubscribe(c *gin.Context) { h.setFromToken(c, true) }

// Get — GET /api/v1/email/preferences?token=…
func (h *EmailPreferencesHandler) Get(c *gin.Context) {
	userID, list, ok := utils.ParseUnsubscribeToken(c.Query("token"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid link"})
		return
	}
	var email string
	var subscribed bool
	err := h.DB.QueryRow(`SELECT email, `+emailListColumn[list]+` FROM users WHERE id = $1`, userID).Scan(&email, &subscribed)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusOK, emailListState{List: list})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read failed"})
		return
	}
	c.JSON(http.StatusOK, emailListState{List: list, Subscribed: subscribed, Email: utils.MaskEmailForLog(email)})
}

type updateEmailPreferencesRequest struct {
	MonthlyRecap *bool `json:"monthly_recap"`
	News         *bool `json:"news"`
}

// UpdateMine — PUT /api/v1/user/email-preferences (signed in).
func (h *EmailPreferencesHandler) UpdateMine(c *gin.Context) {
	userID := middleware.GetUserID(c)
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	var req updateEmailPreferencesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var recap, news bool
	err := h.DB.QueryRow(`
		UPDATE users
		SET email_monthly_recap = COALESCE($1, email_monthly_recap),
		    email_news = COALESCE($2, email_news),
		    updated_at = NOW()
		WHERE id = $3
		RETURNING email_monthly_recap, email_news`, req.MonthlyRecap, req.News, userID).Scan(&recap, &news)
	if err != nil {
		utils.SafeError("email-preferences: update mine: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update email preferences"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"email_monthly_recap": recap, "email_news": news})
}
