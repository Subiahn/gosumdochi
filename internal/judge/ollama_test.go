package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"gosumdochi/internal/llm"
	"gosumdochi/internal/model"
)

type fakeClient struct {
	resp llm.Response
	err  error
	last llm.Request
}

func (f *fakeClient) Generate(_ context.Context, req llm.Request) (llm.Response, error) {
	f.last = req
	return f.resp, f.err
}

func testInput() Input {
	return Input{
		Candidates:   []model.Action{{ID: "C-1", TypedArgs: model.HTTPArgs{AccountRef: "A", URL: "http://labapp:8080/api/docs/d-1"}, HypothesisID: "H-1", RationaleSummary: "cross read"}},
		Hypotheses:   []model.Hypothesis{{ID: "H-1", Claim: "A can read B's doc", Status: model.HypTesting}},
		Observations: []model.Observation{{ID: "O-1", ResultStatus: model.ResultCompleted, EvidenceIDs: []string{"E-1"}, Summary: "HTTP 200"}},
		Remaining:    model.Budgets{MaxCycles: 10, MaxHTTPRequests: 20, MaxModelCalls: 30},
	}
}

func TestJudgeParsesExecute(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: `{"decision":"execute","selected_action_id":"C-1","reason_code":"new_observation_needed","evidence_ids":["E-1"]}`}}
	d, err := (OllamaJudge{Client: c}).Judge(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != model.DecisionExecute || d.SelectedActionID != "C-1" || d.ReasonCode != model.ReasonNewObservationNeeded || len(d.EvidenceIDs) != 1 {
		t.Fatalf("%+v", d)
	}
	if !bytes.Equal(c.last.Schema, outputSchema) {
		t.Error("schema not passed to the client")
	}
}

func TestJudgeParsesStop(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: `{"decision":"stop","reason_code":"exploration_exhausted","evidence_ids":[]}`}}
	d, err := (OllamaJudge{Client: c}).Judge(context.Background(), testInput())
	if err != nil || d.Decision != model.DecisionStop || d.SelectedActionID != "" {
		t.Fatalf("%v %+v", err, d)
	}
}

// Decode-level failures: malformed JSON or a field the Decision type doesn't
// have at all. These are rejected by Judge.Judge itself.
func TestJudgeRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"not json":    "nope",
		"extra field": `{"decision":"stop","reason_code":"exploration_exhausted","evidence_ids":[],"confidence":0.9}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := (OllamaJudge{Client: &fakeClient{resp: llm.Response{Content: content}}}).Judge(context.Background(), testInput()); err == nil {
				t.Fatalf("expected decode error for %q", content)
			}
		})
	}
}

// Schema-shaped but semantically invalid output decodes fine (json.Unmarshal
// doesn't enforce enum/if-then), and is instead rejected downstream by
// model.ValidateDecision, which the agent calls on every Judge result.
func TestJudgeDecodesButDownstreamRejects(t *testing.T) {
	cases := map[string]string{
		"stop with action": `{"decision":"stop","selected_action_id":"C-1","reason_code":"exploration_exhausted","evidence_ids":[]}`,
		"unknown reason":   `{"decision":"stop","reason_code":"made_up","evidence_ids":[]}`,
	}
	refs := model.Refs{Candidates: map[string]model.Action{"C-1": {ID: "C-1"}}, Evidence: map[string]struct{}{}}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := (OllamaJudge{Client: &fakeClient{resp: llm.Response{Content: content}}}).Judge(context.Background(), testInput())
			if err != nil {
				t.Fatalf("Judge itself should not reject: %v", err)
			}
			if model.ValidateDecision(d, refs) == nil {
				t.Fatalf("expected model.ValidateDecision to reject %+v", d)
			}
		})
	}
}

func TestJudgeStripsCodeFence(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: "```json\n{\"decision\":\"inconclusive\",\"reason_code\":\"insufficient_evidence\",\"evidence_ids\":[]}\n```"}}
	d, err := (OllamaJudge{Client: c}).Judge(context.Background(), testInput())
	if err != nil || d.Decision != model.DecisionInconclusive {
		t.Fatalf("%v %+v", err, d)
	}
}

func TestJudgePropagatesClientError(t *testing.T) {
	want := errors.New("ollama: timeout")
	_, err := (OllamaJudge{Client: &fakeClient{err: want}}).Judge(context.Background(), testInput())
	if !errors.Is(err, want) {
		t.Fatalf("%v", err)
	}
}

func TestRenderJudgeInputIncludesContext(t *testing.T) {
	in := testInput()
	in.Feedback = "unknown_reference"
	in.Verifications = []model.Verification{{HypothesisID: "H-1", Outcome: model.OutcomeInconclusive, Reasons: []string{"no cross-account read attempted yet"}}}
	msg := renderJudgeInput(in)
	for _, want := range []string{"unknown_reference", "C-1", "cross read", "inconclusive", "no cross-account read attempted yet", "cycles"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in:\n%s", want, msg)
		}
	}
}

func TestSchemaMatchesConfigsCopy(t *testing.T) {
	canon, err := os.ReadFile("../../configs/schemas/judge-decision.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(canon, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(outputSchema, &b); err != nil {
		t.Fatal(err)
	}
	ca, _ := json.Marshal(a)
	cb, _ := json.Marshal(b)
	if !bytes.Equal(ca, cb) {
		t.Fatal("internal/judge/schema.json has drifted from configs/schemas/judge-decision.schema.json")
	}
}
