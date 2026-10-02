package services

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

func TestValidatePrivateItem(t *testing.T) {
	ok := json.RawMessage(`{"label":"Impôt","category":"tax"}`)
	if err := ValidatePrivateItem("charge:c_123", ok); err != nil {
		t.Fatalf("valid item rejected: %v", err)
	}
	cases := map[string]struct {
		id      string
		payload json.RawMessage
	}{
		"empty id":      {"", ok},
		"bad id chars":  {"../etc", ok},
		"long id":       {strings.Repeat("a", 101), ok},
		"empty payload": {"charge:1", nil},
		"not json":      {"charge:1", json.RawMessage(`{label`)},
		"too big":       {"charge:1", json.RawMessage(`"` + strings.Repeat("x", MaxPrivatePayloadBytes) + `"`)},
	}
	for name, c := range cases {
		if err := ValidatePrivateItem(c.id, c.payload); !errors.Is(err, ErrInvalidPrivateItem) {
			t.Errorf("%s: want ErrInvalidPrivateItem, got %v", name, err)
		}
	}
}
