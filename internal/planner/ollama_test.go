package planner

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
		Goal:       "check cross-account access",
		Hypotheses: []model.Hypothesis{{ID: "H-1", Claim: "A can read B's doc", Status: model.HypTesting}},
		Observations: []model.Observation{
			{ID: "O-1", AccountRef: "B", URL: "http://labapp:8080/api/docs", ResultStatus: model.ResultCompleted,
				ExposedRefs: []string{"http://labapp:8080/api/docs/d-1"}, EvidenceIDs: []string{"E-1"}, Summary: "HTTP 200"},
		},
		Remaining: model.Budgets{MaxCycles: 10, MaxHTTPRequests: 20, MaxModelCalls: 30},
	}
}

func TestPlanParsesAndAssignsIDs(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: `{"candidates":[
		{"tool":"http_request","typed_args":{"method":"GET","url":"http://labapp:8080/api/docs/d-1","account_ref":"A","source_observation_id":"O-1"},"hypothesis_id":"H-1","rationale_summary":"cross read"}
	],"hypothesis_proposals":[{"id":"H-2","claim":"listing leaks owners"}]}`}}
	p := &OllamaPlanner{Client: c}
	out, err := p.Plan(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Candidates) != 1 || out.Candidates[0].ID == "" {
		t.Fatalf("%+v", out.Candidates)
	}
	if out.Candidates[0].Tool != model.ToolHTTPRequest || out.Candidates[0].TypedArgs.URL != "http://labapp:8080/api/docs/d-1" {
		t.Fatalf("%+v", out.Candidates[0])
	}
	if len(out.Proposals) != 1 || out.Proposals[0].ID != "H-2" {
		t.Fatalf("%+v", out.Proposals)
	}

	// A second call must get a different id even for an identical candidate.
	out2, _ := p.Plan(context.Background(), testInput())
	if out2.Candidates[0].ID == out.Candidates[0].ID {
		t.Fatalf("ids not unique across calls: %s", out.Candidates[0].ID)
	}

	if !bytes.Equal(c.last.Schema, outputSchema) {
		t.Error("schema not passed to the client")
	}
}

func TestPlanEmptyCandidates(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: `{"candidates":[],"hypothesis_proposals":[]}`}}
	out, err := (&OllamaPlanner{Client: c}).Plan(context.Background(), testInput())
	if err != nil || len(out.Candidates) != 0 {
		t.Fatalf("%v %+v", err, out)
	}
}

func TestPlanRejectsMalformedOutput(t *testing.T) {
	cases := map[string]string{
		"not json":      "not json at all",
		"extra field":   `{"candidates":[],"hypothesis_proposals":[],"extra":1}`,
		"invented url":  `{"candidates":[{"tool":"http_request","typed_args":{"method":"GET","url":"http://evil.example/","account_ref":"A","source_observation_id":"O-1"},"hypothesis_id":"H-1","rationale_summary":"x"}],"hypothesis_proposals":[]}`,
		"trailing data": `{"candidates":[],"hypothesis_proposals":[]}{}`,
		"wrong tool":    `{"candidates":[{"tool":"shell","typed_args":{"method":"GET","url":"x","account_ref":"A","source_observation_id":"O-1"},"hypothesis_id":"H-1","rationale_summary":"x"}],"hypothesis_proposals":[]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			c := &fakeClient{resp: llm.Response{Content: content}}
			out, err := (&OllamaPlanner{Client: c}).Plan(context.Background(), testInput())
			// These two are not decode errors: json.Unmarshal doesn't enforce
			// const/enum, so a wrong "tool" or out-of-scope "url" is instead
			// rejected downstream by model.ValidateAction/Policy, applied by
			// the agent's candidate filter. Decode must still succeed here.
			if name == "invented url" || name == "wrong tool" {
				if err != nil || len(out.Candidates) != 1 {
					t.Fatalf("expected a decodable (later-rejected) candidate: %v %+v", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a decode error for %q, got %+v", content, out)
			}
		})
	}
}

func TestPlanStripsCodeFence(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: "```json\n{\"candidates\":[],\"hypothesis_proposals\":[]}\n```"}}
	if _, err := (&OllamaPlanner{Client: c}).Plan(context.Background(), testInput()); err != nil {
		t.Fatal(err)
	}
}

func TestPlanPropagatesClientError(t *testing.T) {
	want := errors.New("ollama: timeout")
	c := &fakeClient{err: want}
	_, err := (&OllamaPlanner{Client: c}).Plan(context.Background(), testInput())
	if !errors.Is(err, want) {
		t.Fatalf("%v", err)
	}
}

func TestRenderPlannerInputIncludesFeedback(t *testing.T) {
	in := testInput()
	in.Feedback = "unknown_reference"
	msg := renderPlannerInput(in)
	if !strings.Contains(msg, "unknown_reference") || !strings.Contains(msg, "O-1") || !strings.Contains(msg, "cycles") {
		t.Fatalf("%s", msg)
	}
}

// The verifier's own, code-computed reasons must reach the Planner: they
// name the exact missing evidence more reliably than re-deriving it from
// raw observations (see internal/agent, which now threads Verifications
// through to Plan()).
func TestRenderPlannerInputIncludesVerifierReasons(t *testing.T) {
	in := testInput()
	in.Verifications = []model.Verification{{
		HypothesisID: "H-1", Outcome: model.OutcomeInconclusive,
		Reasons: []string{"B reads A's document: no control: reader's own document response"},
	}}
	msg := renderPlannerInput(in)
	if !strings.Contains(msg, "no control: reader's own document response") || !strings.Contains(msg, "still missing") {
		t.Fatalf("%s", msg)
	}
}

// Schema served to the model must match the canonical config file exactly.
func TestSchemaMatchesConfigsCopy(t *testing.T) {
	canon, err := os.ReadFile("../../configs/schemas/planner-output.schema.json")
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
		t.Fatal("internal/planner/schema.json has drifted from configs/schemas/planner-output.schema.json")
	}
}

func TestSchemaForLimitTightensCandidates(t *testing.T) {
	maxItems := func(schema []byte) float64 {
		var doc map[string]any
		if err := json.Unmarshal(schema, &doc); err != nil {
			t.Fatal(err)
		}
		return doc["properties"].(map[string]any)["candidates"].(map[string]any)["maxItems"].(float64)
	}
	base := maxItems(outputSchema)
	if got := maxItems(schemaForLimit(3)); got != 3 {
		t.Fatalf("maxItems = %v, want 3", got)
	}
	if got := maxItems(schemaForLimit(0)); got != base {
		t.Fatalf("limit<=0 must keep the static ceiling: got %v, want %v", got, base)
	}
	if got := maxItems(schemaForLimit(1000)); got != base {
		t.Fatalf("a limit above the static ceiling must not widen it: got %v, want %v", got, base)
	}
}

// The agent's actual per-cycle candidate cap must reach the model: a
// mismatched static schema lets it legally emit more than the agent will
// accept, which then loses the whole batch to CodeTooMany.
func TestPlanUsesActualCandidateLimit(t *testing.T) {
	c := &fakeClient{resp: llm.Response{Content: `{"candidates":[],"hypothesis_proposals":[]}`}}
	in := testInput()
	in.Remaining.MaxCandidates = 3
	if _, err := (&OllamaPlanner{Client: c}).Plan(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(c.last.Schema, &doc); err != nil {
		t.Fatal(err)
	}
	if got := doc["properties"].(map[string]any)["candidates"].(map[string]any)["maxItems"].(float64); got != 3 {
		t.Fatalf("schema sent to the model has maxItems=%v, want 3", got)
	}
}
