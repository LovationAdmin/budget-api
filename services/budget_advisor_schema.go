package services

// budgetProposalSchema constrains the advisor's answer (structured outputs) to
// the BudgetProposal contract, so the JSON is always complete and parseable
// and enum fields are always values the UI knows. Property order follows the
// prompt's few-shot example; the progress tracker relies on it.
const budgetProposalSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": [
    "methodChosen", "methodRationale", "accountStructure", "accountRationale",
    "monthlyAllocation", "perMember", "savingsEnvelopes", "variableIncomePolicy",
    "separationHandling", "feasibility", "lifeEventNotes", "vehicleSuggestions",
    "assumptionsMade", "openQuestions", "disclaimer", "summary"
  ],
  "properties": {
    "methodChosen": { "type": "string", "enum": ["prorata", "equal", "equalized_reste", "all_common"] },
    "methodRationale": { "type": "string" },
    "accountStructure": { "type": "string", "enum": ["three_accounts", "all_common_equal_pocket"] },
    "accountRationale": { "type": "string" },
    "monthlyAllocation": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["category", "label", "amount", "type", "fundedBy"],
        "properties": {
          "category": { "type": "string" },
          "label": { "type": "string" },
          "amount": { "type": "number" },
          "type": {
            "type": "string",
            "enum": ["common_charge", "savings_safety", "savings_projects", "vacations", "pocket_money", "personal"]
          },
          "fundedBy": {
            "type": "array",
            "items": {
              "type": "object",
              "additionalProperties": false,
              "required": ["memberId", "amount"],
              "properties": {
                "memberId": { "type": "string" },
                "amount": { "type": "number" }
              }
            }
          },
          "notes": { "type": "string" }
        }
      }
    },
    "perMember": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["memberId", "monthlyContribution", "resteAVivre", "pocketMoney", "personalSavingsCapacity", "feasibility"],
        "properties": {
          "memberId": { "type": "string" },
          "monthlyContribution": { "type": "number" },
          "resteAVivre": { "type": "number" },
          "pocketMoney": { "type": "number" },
          "personalSavingsCapacity": { "type": "number" },
          "feasibility": { "type": "string", "enum": ["ok", "tight", "infeasible"] }
        }
      }
    },
    "savingsEnvelopes": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "priority", "monthlyContribution"],
        "properties": {
          "name": { "type": "string" },
          "priority": { "type": "string", "enum": ["safety", "high", "medium", "low"] },
          "targetAmount": { "type": "number" },
          "horizonMonths": { "type": "integer" },
          "monthlyContribution": { "type": "number" },
          "vehicleSuggestion": { "type": "string" }
        }
      }
    },
    "variableIncomePolicy": { "type": "string" },
    "separationHandling": {
      "type": "object",
      "additionalProperties": false,
      "required": ["approach", "note"],
      "properties": {
        "approach": { "type": "string", "enum": ["equal_5050", "contribution_ledger", "deferred_to_marriage"] },
        "note": { "type": "string" }
      }
    },
    "feasibility": {
      "type": "object",
      "additionalProperties": false,
      "required": ["status", "issues", "suggestedLevers"],
      "properties": {
        "status": { "type": "string", "enum": ["ok", "tight", "infeasible"] },
        "bindingMemberId": { "type": "string" },
        "issues": { "type": "array", "items": { "type": "string" } },
        "suggestedLevers": { "type": "array", "items": { "type": "string" } }
      }
    },
    "lifeEventNotes": { "type": "array", "items": { "type": "string" } },
    "vehicleSuggestions": { "type": "array", "items": { "type": "string" } },
    "assumptionsMade": { "type": "array", "items": { "type": "string" } },
    "openQuestions": { "type": "array", "items": { "type": "string" } },
    "disclaimer": { "type": "string" },
    "summary": { "type": "string" }
  }
}`
