package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sseEvent formats one Messages API stream event.
func sseEvent(payload string) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(payload), &head)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", head.Type, payload)
}

// streamOf builds a complete stream answering text, chunked, after a
// thinking block.
func streamOf(text, stopReason string, chunk int) string {
	var b strings.Builder
	b.WriteString(sseEvent(`{"type":"message_start","message":{"model":"claude-sonnet-5-5","usage":{"input_tokens":100}}}`))
	b.WriteString(sseEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))
	b.WriteString(sseEvent(`{"type":"content_block_stop","index":0}`))
	b.WriteString(sseEvent(`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`))
	for i := 0; i < len(text); i += chunk {
		end := i + chunk
		if end > len(text) {
			end = len(text)
		}
		delta, _ := json.Marshal(map[string]any{
			"type": "content_block_delta", "index": 1,
			"delta": map[string]string{"type": "text_delta", "text": text[i:end]},
		})
		b.WriteString(sseEvent(string(delta)))
	}
	b.WriteString(sseEvent(`{"type":"content_block_stop","index":1}`))
	b.WriteString(sseEvent(fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":4321}}`, stopReason)))
	b.WriteString(sseEvent(`{"type":"message_stop"}`))
	return b.String()
}

func testService(url string) *ClaudeAIService {
	s := NewClaudeAIService()
	s.apiKey = "test-key"
	s.baseURL = url
	s.streamIdleTimeout = 2 * time.Second
	return s
}

func TestStream_AccumulatesTextAndReportsThinking(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		if r.Header.Get("anthropic-beta") != "server-side-fallback-2026-07-01" {
			t.Errorf("missing fallback beta header, got %q", r.Header.Get("anthropic-beta"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, streamOf(`{"hello":"world"}`, "end_turn", 3))
	}))
	defer srv.Close()

	s := testService(srv.URL)
	var kinds []string
	res, err := s.Stream(context.Background(), ClaudeRequest{
		Model: "claude-sonnet-5-5", MaxTokens: 100,
		Messages:     []ClaudeMessage{{Role: "user", Content: "hi"}},
		OutputConfig: &OutputConfig{Effort: "low", Format: JSONSchemaFormat(`{"type":"object"}`)},
		Fallbacks:    refusalFallback("claude-sonnet-5-5"),
	}, func(ev StreamEvent) { kinds = append(kinds, ev.Kind) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if res.Text != `{"hello":"world"}` || res.StopReason != "end_turn" || res.OutputTokens != 4321 {
		t.Fatalf("unexpected result %+v", res)
	}
	if kinds[0] != "thinking" || kinds[len(kinds)-1] != "text" {
		t.Fatalf("unexpected events %v", kinds)
	}
	if gotBody["stream"] != true {
		t.Fatalf("request must ask for a stream, body=%v", gotBody)
	}
	oc := gotBody["output_config"].(map[string]any)
	if oc["effort"] != "low" || oc["format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("bad output_config %v", oc)
	}
	if gotBody["fallbacks"] != "default" {
		t.Fatalf("fallbacks not sent: %v", gotBody["fallbacks"])
	}
}

func TestStream_InStreamOverloadIsTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sseEvent(`{"type":"message_start","message":{"model":"m","usage":{"input_tokens":1}}}`))
		io.WriteString(w, sseEvent(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer srv.Close()

	_, err := testService(srv.URL).Stream(context.Background(), ClaudeRequest{Model: "m", MaxTokens: 10}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 529 || !IsTransientAIError(err) {
		t.Fatalf("want transient 529, got %v", err)
	}
}

func TestStream_HTTPErrorIsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","message":"model: nope"}}`)
	}))
	defer srv.Close()

	_, err := testService(srv.URL).Stream(context.Background(), ClaudeRequest{Model: "nope", MaxTokens: 10}, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || apiErr.Type != "not_found_error" || IsTransientAIError(err) {
		t.Fatalf("want permanent 404, got %v", err)
	}
}

func TestStream_StallIsDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sseEvent(`{"type":"message_start","message":{"model":"m","usage":{"input_tokens":1}}}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	s := testService(srv.URL)
	s.streamIdleTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := s.Stream(context.Background(), ClaudeRequest{Model: "m", MaxTokens: 10}, nil)
	if !errors.Is(err, ErrStreamStalled) || !IsTransientAIError(err) {
		t.Fatalf("want stall, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("stall detection took %v", time.Since(start))
	}
}

func TestDo_RetriesTransientThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("retry-after", "1")
			w.WriteHeader(529)
			io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
			return
		}
		io.WriteString(w, `{"model":"m","stop_reason":"end_turn","content":[{"type":"thinking","thinking":""},{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":2}}`)
	}))
	defer srv.Close()

	res, err := testService(srv.URL).Do(context.Background(), ClaudeRequest{Model: "m", MaxTokens: 10})
	if err != nil || res.Text != "ok" || calls.Load() != 2 {
		t.Fatalf("res=%+v err=%v calls=%d", res, err, calls.Load())
	}
}

func TestDo_RefusalIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"model":"m","stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},"content":[],"usage":{}}`)
	}))
	defer srv.Close()

	_, err := testService(srv.URL).Do(context.Background(), ClaudeRequest{Model: "m", MaxTokens: 10})
	if !errors.Is(err, ErrRefusal) || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestRefusalFallback_OnlyForSupportedModels(t *testing.T) {
	if refusalFallback("claude-sonnet-5-5") != "default" {
		t.Fatal("sonnet 5.5 should use the default fallback")
	}
	if refusalFallback("claude-sonnet-5") != "" || refusalFallback("claude-haiku-4-5") != "" {
		t.Fatal("unsupported models must not send fallbacks")
	}
	t.Setenv("CLAUDE_REFUSAL_FALLBACK", "off")
	if refusalFallback("claude-sonnet-5-5") != "" {
		t.Fatal("CLAUDE_REFUSAL_FALLBACK=off must disable it")
	}
}
