// services/push_service.go
// ============================================================================
// EXPO PUSH SERVICE
// ============================================================================
// Sends push notifications to a user's registered devices via Expo Push API.
// https://docs.expo.dev/push-notifications/sending-notifications/
//
// Usage:
//   svc := services.NewPushService(db)
//   svc.NotifyUser(ctx, userID, "Nouveau membre", "Marie a rejoint Budget Famille", map[string]any{...})
//
// Implementation notes:
//   - Best-effort: errors are logged, never returned to the caller. Push
//     delivery should NEVER block the main request flow.
//   - Tokens marked as DeviceNotRegistered by Expo are removed from the table.
//   - Body is hardcoded to default sound/priority — extend if you need silent
//     pushes (data-only) or rich content.
// ============================================================================

package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/LovationAdmin/budget-api/utils"
)

const expoPushURL = "https://exp.host/--/api/v2/push/send"

type PushService struct {
	db     *sql.DB
	client *http.Client
}

func NewPushService(db *sql.DB) *PushService {
	return &PushService{
		db:     db,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type expoMessage struct {
	To       string         `json:"to"`
	Title    string         `json:"title"`
	Body     string         `json:"body"`
	Sound    string         `json:"sound,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Priority string         `json:"priority,omitempty"`
}

type expoTicket struct {
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type expoResponse struct {
	Data []expoTicket `json:"data"`
}

// NotifyUser sends a push to every device registered by the given user.
// Title and body are user-facing strings; data is delivered to the app as JSON
// (use it to deep-link to a budget, charge, etc).
func (s *PushService) NotifyUser(ctx context.Context, userID, title, body string, data map[string]any) {
	if title == "" && body == "" {
		return
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT expo_push_token FROM user_devices WHERE user_id = $1
	`, userID)
	if err != nil {
		utils.SafeWarn("push: query devices: %v", err)
		return
	}
	defer rows.Close()

	tokens := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err == nil && strings.HasPrefix(t, "ExponentPushToken") {
			tokens = append(tokens, t)
		}
	}
	if len(tokens) == 0 {
		return
	}

	messages := make([]expoMessage, 0, len(tokens))
	for _, t := range tokens {
		messages = append(messages, expoMessage{
			To: t, Title: title, Body: body,
			Sound: "default", Priority: "high", Data: data,
		})
	}

	payload, err := json.Marshal(messages)
	if err != nil {
		utils.SafeWarn("push: marshal: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, expoPushURL, bytes.NewReader(payload))
	if err != nil {
		utils.SafeWarn("push: build req: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		utils.SafeWarn("push: send: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		utils.SafeWarn("push: HTTP %d", resp.StatusCode)
		return
	}

	var er expoResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		utils.SafeWarn("push: decode: %v", err)
		return
	}

	for i, ticket := range er.Data {
		if ticket.Status != "ok" && ticket.Details != nil {
			if reason, ok := ticket.Details["error"].(string); ok && reason == "DeviceNotRegistered" {
				if i < len(tokens) {
					_, _ = s.db.ExecContext(ctx,
						`DELETE FROM user_devices WHERE expo_push_token = $1`, tokens[i])
					utils.SafeInfo("push: removed unregistered device")
				}
			}
		}
	}

	_ = fmt.Sprint // silence unused if utils logs are no-op in tests
}
