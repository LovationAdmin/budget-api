package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sampleInput() HouseholdInput {
	return HouseholdInput{
		HouseholdType: "couple",
		Country:       "FR",
		Members: []AdvisorMember{
			{ID: "A", Label: "A", NetIncome: 3400},
			{ID: "B", Label: "B", NetIncome: 2250},
		},
		Objectives:           []AdvisorObjective{{Label: "Mariage", Priority: "high"}},
		WantsPersonalSavings: false,
		FreeText:             "Couple qui veut fusionner.",
	}
}

func TestBuildAdvisorMessages_EndsWithUser(t *testing.T) {
	// The Claude 5 family rejects assistant-message prefill: the conversation
	// must end with a user message. Guards against reintroducing the prefill.
	msgs, err := buildAdvisorMessages(sampleInput())
	if err != nil {
		t.Fatalf("buildAdvisorMessages returned error: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected messages, got none")
	}
	if last := msgs[len(msgs)-1]; last.Role != "user" {
		t.Fatalf("last message role = %q, want \"user\"", last.Role)
	}
}

func TestParseProposal_StripsMarkdownFences(t *testing.T) {
	raw := "Voici la proposition :\n```json\n" + advisorFewShotOutput + "\n```\n"
	p, err := parseProposal(raw)
	if err != nil {
		t.Fatalf("parseProposal returned error: %v", err)
	}
	if p.MethodChosen != "all_common" {
		t.Fatalf("expected methodChosen all_common, got %q", p.MethodChosen)
	}
	if len(p.MonthlyAllocation) == 0 {
		t.Fatalf("expected monthlyAllocation to be populated")
	}
}

func TestValidateProposal_AcceptsFewShotOutput(t *testing.T) {
	p, err := parseProposal(advisorFewShotOutput)
	if err != nil {
		t.Fatalf("parseProposal returned error: %v", err)
	}
	if err := validateProposal(p, sampleInput()); err != nil {
		t.Fatalf("validateProposal rejected valid proposal: %v", err)
	}
}

func TestValidateProposal_RejectsEmptyAllocation(t *testing.T) {
	p, _ := parseProposal(advisorFewShotOutput)
	p.MonthlyAllocation = nil
	if err := validateProposal(p, sampleInput()); err == nil {
		t.Fatalf("expected validation to reject empty monthlyAllocation")
	}
}

func TestValidateProposal_RejectsEmptySummary(t *testing.T) {
	p, _ := parseProposal(advisorFewShotOutput)
	p.Summary = ""
	if err := validateProposal(p, sampleInput()); err == nil {
		t.Fatalf("expected validation to reject empty summary")
	}
}

func TestSanitizeProposal_CoercesBadFeasibilityAndDisclaimer(t *testing.T) {
	p, _ := parseProposal(advisorFewShotOutput)
	p.Feasibility.Status = "weird"
	p.PerMember[0].Feasibility = "nope"
	p.Disclaimer = ""
	sanitizeProposal(p)
	if p.Feasibility.Status != "tight" {
		t.Fatalf("feasibility.status = %q, want tight", p.Feasibility.Status)
	}
	if p.PerMember[0].Feasibility != "tight" {
		t.Fatalf("perMember feasibility = %q, want tight", p.PerMember[0].Feasibility)
	}
	if p.Disclaimer == "" {
		t.Fatalf("disclaimer should have been filled with a default")
	}
	// A sanitized proposal must pass validation.
	if err := validateProposal(p, sampleInput()); err != nil {
		t.Fatalf("sanitized proposal failed validation: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Streaming generation: progress, retry policy, error classification
// ---------------------------------------------------------------------------

func advisorWithServer(t *testing.T, handler http.HandlerFunc) *BudgetAdvisorService {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	svc := NewBudgetAdvisorService(testService(srv.URL))
	return svc
}

func TestProgressTracker_StagesInOrderIgnoringMemberFeasibility(t *testing.T) {
	var stages []string
	var method string
	tr := &progressTracker{emit: func(p AdvisorProgress) {
		if p.Method != "" {
			method = p.Method
			return
		}
		stages = append(stages, p.Stage)
	}}
	// Feed the few-shot answer chunk by chunk, as a stream would.
	text := advisorFewShotOutput
	for i := 1; i <= len(text); i += 7 {
		tr.onEvent(StreamEvent{Kind: "text", Text: text[:i]})
	}
	tr.onEvent(StreamEvent{Kind: "text", Text: text})

	want := []string{AdvisorStageMethod, AdvisorStageAllocation, AdvisorStageMembers, AdvisorStageSavings, AdvisorStageFeasibility, AdvisorStageSummary}
	if strings.Join(stages, ",") != strings.Join(want, ",") {
		t.Fatalf("stages = %v, want %v", stages, want)
	}
	if method != "all_common" {
		t.Fatalf("method = %q", method)
	}
}

func TestGenerate_StreamsProgressAndParsesStructuredOutput(t *testing.T) {
	svc := advisorWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.OutputConfig == nil || body.OutputConfig.Effort != "medium" || body.OutputConfig.Format == nil {
			t.Errorf("advisor must send effort + json schema, got %+v", body.OutputConfig)
		}
		io.WriteString(w, streamOf(advisorFewShotOutput, "end_turn", 40))
	})

	var stages []string
	p, err := svc.GenerateProposalWithProgress(context.Background(), sampleInput(), func(ev AdvisorProgress) {
		stages = append(stages, ev.Stage)
	})
	if err != nil {
		t.Fatalf("GenerateProposalWithProgress: %v", err)
	}
	if p.MethodChosen != "all_common" || stages[0] != AdvisorStageAnalyzing || stages[len(stages)-1] != AdvisorStageSummary {
		t.Fatalf("method=%q stages=%v", p.MethodChosen, stages)
	}
}

func TestGenerate_MaxTokensRetriesWithLowEffort(t *testing.T) {
	var efforts []string
	svc := advisorWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		efforts = append(efforts, body.OutputConfig.Effort)
		if len(efforts) == 1 {
			// The whole budget went to thinking: no text at all.
			io.WriteString(w, streamOf("", "max_tokens", 10))
			return
		}
		io.WriteString(w, streamOf(advisorFewShotOutput, "end_turn", 500))
	})

	var sawRetry bool
	p, err := svc.GenerateProposalWithProgress(context.Background(), sampleInput(), func(ev AdvisorProgress) {
		sawRetry = sawRetry || ev.Stage == AdvisorStageRetry
	})
	if err != nil || p == nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if strings.Join(efforts, ",") != "medium,low" || !sawRetry {
		t.Fatalf("efforts=%v sawRetry=%v", efforts, sawRetry)
	}
}

func TestGenerate_ConfigErrorFailsFastWithoutRetry(t *testing.T) {
	var calls int
	svc := advisorWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(404)
		io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","message":"model: x"}}`)
	})

	_, err := svc.GenerateProposal(context.Background(), sampleInput())
	var advErr *AdvisorError
	if !errors.As(err, &advErr) || advErr.Code != AdvisorErrUnavailable || advErr.Retryable || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestGenerate_NoRetryWhenDeadlineTooClose(t *testing.T) {
	var calls int
	svc := advisorWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(529)
		io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	})

	ctx, cancel := context.WithTimeout(context.Background(), advisorMinRetryWindow-5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := svc.GenerateProposal(ctx, sampleInput())
	var advErr *AdvisorError
	if !errors.As(err, &advErr) || advErr.Code != AdvisorErrBusy || !advErr.Retryable || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("should fail fast, took %v", time.Since(start))
	}
}

func TestGenerate_DeadlineIsReportedAsTimeout(t *testing.T) {
	svc := advisorWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sseEvent(`{"type":"message_start","message":{"model":"m","usage":{"input_tokens":1}}}`))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := svc.GenerateProposal(ctx, sampleInput())
	var advErr *AdvisorError
	if !errors.As(err, &advErr) || advErr.Code != AdvisorErrTimeout {
		t.Fatalf("want timeout, got %v", err)
	}
	if advErr.UserMessage() == "" || advErr.HTTPStatus() != 504 {
		t.Fatalf("bad user mapping: %q %d", advErr.UserMessage(), advErr.HTTPStatus())
	}
}

func TestSanitizeProposal_FillsMissingLists(t *testing.T) {
	p := &BudgetProposal{MonthlyAllocation: []AllocationLine{{Label: "x"}}}
	sanitizeProposal(p)
	if p.Feasibility.Issues == nil || p.OpenQuestions == nil || p.SavingsEnvelopes == nil || p.MonthlyAllocation[0].FundedBy == nil {
		t.Fatalf("nil lists would be null in JSON and crash the UI: %+v", p)
	}
}

func TestGenerate_EndlessThinkingIsCutAndRetriedAtLowEffort(t *testing.T) {
	var efforts []string
	svc := advisorWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body ClaudeRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		efforts = append(efforts, body.OutputConfig.Effort)
		if len(efforts) == 1 {
			// Thinks forever (pings keep the stream alive), never answers.
			io.WriteString(w, sseEvent(`{"type":"message_start","message":{"model":"m","usage":{"input_tokens":1}}}`))
			io.WriteString(w, sseEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`))
			for {
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-time.After(50 * time.Millisecond):
					io.WriteString(w, sseEvent(`{"type":"ping"}`))
				}
			}
		}
		io.WriteString(w, streamOf(advisorFewShotOutput, "end_turn", 500))
	})
	svc.firstTextTimeout = 300 * time.Millisecond

	start := time.Now()
	p, err := svc.GenerateProposal(context.Background(), sampleInput())
	if err != nil || p == nil {
		t.Fatalf("expected success after cutting the endless thinking, got %v", err)
	}
	if strings.Join(efforts, ",") != "medium,low" {
		t.Fatalf("efforts=%v", efforts)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}
