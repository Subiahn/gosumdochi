package judge

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"gosumdochi/internal/llm"
	"gosumdochi/internal/model"
)

//go:embed schema.json
var outputSchema []byte

const systemPrompt = `You are the Judge in the same local access-control testing agent. You choose
among the action candidates the Planner already proposed, or decide to stop.
You cannot build requests yourself, and you cannot mark a hypothesis
confirmed: a separate, code-driven verifier does that after each request and
reports its outcome back to you as context.

Rules:
- "decision" is one of "execute", "stop", "inconclusive".
- "selected_action_id" is ALWAYS present. If "execute", it MUST be copied
  exactly from one of the given candidate ids. Otherwise it MUST be the
  empty string "" -- never omit the field, and never invent an id.
- "reason_code" explains why. Use "hypothesis_verified" only when a
  verification given to you already shows outcome "confirmed" for the
  relevant hypothesis; you are not the one granting confirmation.
- "evidence_ids" may cite ids that already appear in an observation's
  "evidence_ids" or a verification's baseline/comparison ids; an empty list
  is fine.
- Prefer "execute" while a useful candidate remains and budget allows.
  Choose "stop" with "exploration_exhausted" when no candidate would add new
  evidence. Choose "inconclusive" with "insufficient_evidence" if further
  testing is unlikely to resolve the hypothesis within budget.
- Respond with a single JSON object matching the given schema. No prose, no
  markdown fences, no extra fields.`

// OllamaJudge drives Judge.Judge with a chat-completions llm.Client under a
// JSON schema. Its output still passes through model.ValidateDecision and
// the agent's own confirmation check before anything is stored as a decision.
type OllamaJudge struct {
	Client llm.Client
}

func (j OllamaJudge) Judge(ctx context.Context, in Input) (model.Decision, error) {
	resp, err := j.Client.Generate(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: renderJudgeInput(in)},
		},
		Schema: outputSchema,
	})
	if err != nil {
		return model.Decision{}, err
	}
	return model.DecodeStrict[model.Decision]([]byte(stripFences(resp.Content)))
}

type candidateView struct {
	ID        string `json:"id"`
	Account   string `json:"account_ref"`
	URL       string `json:"url"`
	Hyp       string `json:"hypothesis_id"`
	Rationale string `json:"rationale_summary"`
}

type verificationView struct {
	HypothesisID string   `json:"hypothesis_id"`
	Outcome      string   `json:"outcome"`
	Reasons      []string `json:"reasons,omitempty"`
}

func renderJudgeInput(in Input) string {
	cands := make([]candidateView, len(in.Candidates))
	for i, c := range in.Candidates {
		cands[i] = candidateView{ID: c.ID, Account: string(c.TypedArgs.AccountRef), URL: c.TypedArgs.URL, Hyp: c.HypothesisID, Rationale: c.RationaleSummary}
	}
	hyps := make([]struct {
		ID     string `json:"id"`
		Claim  string `json:"claim"`
		Status string `json:"status"`
	}, len(in.Hypotheses))
	for i, h := range in.Hypotheses {
		hyps[i].ID, hyps[i].Claim, hyps[i].Status = h.ID, h.Claim, string(h.Status)
	}
	obs := make([]struct {
		ID          string   `json:"id"`
		Status      string   `json:"result_status"`
		EvidenceIDs []string `json:"evidence_ids,omitempty"`
		Summary     string   `json:"summary,omitempty"`
	}, len(in.Observations))
	for i, o := range in.Observations {
		obs[i].ID, obs[i].Status, obs[i].EvidenceIDs, obs[i].Summary = o.ID, string(o.ResultStatus), o.EvidenceIDs, o.Summary
	}
	vers := make([]verificationView, len(in.Verifications))
	for i, v := range in.Verifications {
		vers[i] = verificationView{HypothesisID: v.HypothesisID, Outcome: string(v.Outcome), Reasons: v.Reasons}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Remaining budget: %d cycles, %d http requests, %d model calls.\n\n",
		in.Remaining.MaxCycles, in.Remaining.MaxHTTPRequests, in.Remaining.MaxModelCalls)
	writeJSON(&b, "Candidates", cands)
	writeJSON(&b, "Hypotheses", hyps)
	writeJSON(&b, "Verifications so far", vers)
	writeJSON(&b, "Recent observations (oldest first)", obs)
	if in.Feedback != "" {
		fmt.Fprintf(&b, "\nYour previous response was rejected (code: %s). Review the rules and respond again with a corrected JSON object only.\n", in.Feedback)
	}
	return b.String()
}

func writeJSON(b *strings.Builder, label string, v any) {
	j, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintf(b, "%s:\n%s\n\n", label, j)
}

// stripFences removes a ```json ... ``` wrapper some models add despite
// instructions not to.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 && !strings.Contains(s[:nl], "{") {
		s = s[nl+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

var _ Judge = OllamaJudge{}
