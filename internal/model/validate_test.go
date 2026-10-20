package model

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func fixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "tests", "fixtures", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testRefs() Refs {
	return Refs{
		Candidates:   map[string]Action{"A-007": {ID: "A-007"}},
		Evidence:     map[string]struct{}{"E-012": {}, "E-015": {}},
		Hypotheses:   map[string]struct{}{"H-001": {}},
		Observations: map[string]Observation{"O-003": {ID: "O-003", ExposedRefs: []string{"http://labapp:8080/api/docs/42"}}},
	}
}

func TestDecision(t *testing.T) {
	cases := []struct {
		file string
		want ErrorCode // "" = valid
	}{
		{"judge/valid_execute.json", ""},
		{"judge/unknown_candidate.json", CodeUnknownRef},
		{"judge/unknown_evidence.json", CodeUnknownRef},
		{"judge/bad_reason.json", CodeUnknownEnum},
		{"judge/confirm_attempt.json", CodeUnknownEnum},
		{"judge/stop_with_action.json", CodeInvalidValue},
		{"judge/extra_field.json", CodeDecode},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			d, err := DecodeStrict[Decision](fixture(t, c.file))
			if err == nil {
				err = ValidateDecision(d, testRefs())
			}
			if got := CodeOf(err); got != c.want {
				t.Fatalf("code = %q (%v), want %q", got, err, c.want)
			}
		})
	}
}

func TestAction(t *testing.T) {
	cases := []struct {
		file string
		want ErrorCode
	}{
		{"action/valid.json", ""},
		{"action/invented_url.json", CodeUntrustedRef},
		{"action/post_method.json", CodeInvalidValue},
		{"action/no_source.json", CodeMissingField},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			a, err := DecodeStrict[Action](fixture(t, c.file))
			if err == nil {
				err = ValidateAction(a, testRefs())
			}
			if got := CodeOf(err); got != c.want {
				t.Fatalf("code = %q (%v), want %q", got, err, c.want)
			}
		})
	}
}

func TestCandidateLimitAndDuplicates(t *testing.T) {
	a, _ := DecodeStrict[Action](fixture(t, "action/valid.json"))
	refs := testRefs()
	if err := ValidateCandidates([]Action{a, a}, 3, refs); CodeOf(err) != CodeInvalidValue {
		t.Fatalf("duplicate: %v", err)
	}
	b, c, d := a, a, a
	b.ID, c.ID, d.ID = "A-1", "A-2", "A-3"
	if err := ValidateCandidates([]Action{a, b, c, d}, 3, refs); CodeOf(err) != CodeTooMany {
		t.Fatalf("limit: %v", err)
	}
	if err := ValidateCandidates([]Action{a, b}, 3, refs); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeStrictRejectsTrailingData(t *testing.T) {
	if _, err := DecodeStrict[Decision]([]byte(`{"decision":"stop","reason_code":"budget_low","evidence_ids":[]} {}`)); CodeOf(err) != CodeDecode {
		t.Fatalf("got %v", err)
	}
}

func TestFinding(t *testing.T) {
	vs := map[string]Verification{
		"V-1": {ID: "V-1", HypothesisID: "H-001", Outcome: OutcomeConfirmed},
		"V-2": {ID: "V-2", HypothesisID: "H-001", Outcome: OutcomeInconclusive},
	}
	refs := testRefs()
	ok := Finding{HypothesisID: "H-001", VerificationID: "V-1", EvidenceIDs: []string{"E-012"}}
	if err := ValidateFinding(ok, vs, refs); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]Finding{
		"no verification":    {HypothesisID: "H-001", VerificationID: "V-9", EvidenceIDs: []string{"E-012"}},
		"not confirmed":      {HypothesisID: "H-001", VerificationID: "V-2", EvidenceIDs: []string{"E-012"}},
		"hypothesis differs": {HypothesisID: "H-002", VerificationID: "V-1", EvidenceIDs: []string{"E-012"}},
		"no evidence":        {HypothesisID: "H-001", VerificationID: "V-1"},
		"unknown evidence":   {HypothesisID: "H-001", VerificationID: "V-1", EvidenceIDs: []string{"E-404"}},
	} {
		if ValidateFinding(f, vs, refs) == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestTransitions(t *testing.T) {
	if CanTransition(HypProposed, HypConfirmed) {
		t.Error("proposed must not jump to confirmed")
	}
	if !CanTransition(HypTesting, HypConfirmed) || CanTransition(HypConfirmed, HypTesting) {
		t.Error("unexpected transition rules")
	}
}

// The JSON Schemas and Go enums must not drift apart.
func TestSchemaEnumsMatchGo(t *testing.T) {
	var s struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(fixtureSchema(t, "judge-decision.schema.json"), &s); err != nil {
		t.Fatal(err)
	}
	same := func(name string, schema, goSet []string) {
		sort.Strings(schema)
		sort.Strings(goSet)
		if len(schema) != len(goSet) {
			t.Fatalf("%s: schema %v vs go %v", name, schema, goSet)
		}
		for i := range schema {
			if schema[i] != goSet[i] {
				t.Fatalf("%s: schema %v vs go %v", name, schema, goSet)
			}
		}
	}
	var kinds, reasons []string
	for _, k := range DecisionKinds {
		kinds = append(kinds, string(k))
	}
	for _, r := range ReasonCodes {
		reasons = append(reasons, string(r))
	}
	same("decision", s.Properties["decision"].Enum, kinds)
	same("reason_code", s.Properties["reason_code"].Enum, reasons)
}

func fixtureSchema(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "configs", "schemas", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
