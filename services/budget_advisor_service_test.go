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

func TestValidateProposal_RejectsBadMethod(t *testing.T) {
	p, _ := parseProposal(advisorFewShotOutput)
	p.MethodChosen = "not_a_method"
	if err := validateProposal(p, sampleInput()); err == nil {
		t.Fatalf("expected validation to reject invalid methodChosen")
	}
}

func TestValidateProposal_RejectsFundedByMismatch(t *testing.T) {
	p, _ := parseProposal(advisorFewShotOutput)
	// Break the first line's fundedBy sum.
	p.MonthlyAllocation[0].FundedBy = []FundedBy{{MemberID: "A", Amount: 1}}
	if err := validateProposal(p, sampleInput()); err == nil {
		t.Fatalf("expected validation to reject fundedBy sum mismatch")
	}
}

func TestValidateProposal_RejectsMemberCountMismatch(t *testing.T) {
	p, _ := parseProposal(advisorFewShotOutput)
	in := sampleInput()
	in.Members = in.Members[:1] // only one member, proposal has two
	if err := validateProposal(p, in); err == nil {
		t.Fatalf("expected validation to reject perMember count mismatch")
	}
}
