package services

import "testing"

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
