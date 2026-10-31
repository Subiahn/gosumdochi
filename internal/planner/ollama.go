package planner

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"gosumdochi/internal/llm"
	"gosumdochi/internal/model"
)

//go:embed schema.json
var outputSchema []byte

const systemPrompt = `You are the Planner in a local security-testing agent that checks access
control between two test accounts ("A" and "B") on a small practice web
application. You never execute requests yourself; you only propose candidates
for a code-enforced pipeline that validates and may reject them.

Rules:
- Only propose the "http_request" tool with method "GET".
- "typed_args.url" MUST be copied exactly, character for character, from the
  "exposed_refs" list of the observation you name as "typed_args.source_observation_id".
  Never invent, guess, or edit a URL, document id, or query string. If no
  observation currently exposes a useful ref, propose zero candidates.
- "typed_args.account_ref" is "A" or "B": the account whose session should
  make the request.
- "hypothesis_id" must be one of the hypothesis ids you were given.
- The prompt below includes "What the verifier is still missing": the
  verifier's own, code-computed reason why a hypothesis is not yet
  confirmed or rejected, after the most recent relevant attempt. Treat this
  as your primary, concrete to-do list -- it names the exact missing piece
  (e.g. a specific account's own baseline read) more reliably than you can
  re-derive it from the raw observations alone. When it is present, address
  it before anything else, for the hypothesis_id it names.
- A listing endpoint such as "/api/docs" can be requested again with a
  DIFFERENT account_ref than before: the same URL, authenticated as the
  other account, is a new, valid, and often necessary request (it is not a
  repeat). The verifier needs each account's own listing and its own
  document read, authenticated as itself, before a cross-account read of the
  same document means anything.
- Prioritize, in order, across BOTH accounts: (1) for EACH account, first
  list its own data AND read at least one of its own items, authenticated as
  itself, if you have not already done both for that account -- this is the
  only way to learn that account's own marker, without which a later
  cross-account read of the same item cannot be told apart from a session
  mix-up; (2) only once BOTH accounts have done (1), read a resource that
  belongs to one account while authenticated as the OTHER account; (3) one
  independent repeat of a successful cross-account read, to rule out a
  fluke.
- You may suggest new hypotheses in "hypothesis_proposals" (short unique id,
  one-sentence claim). This is optional.
- "candidates" has a hard limit for this turn, stated in the prompt below as
  "max candidates". Never exceed it: a response with too many candidates is
  rejected in full, none of them get run, and you only waste a turn. If you
  have more good candidates than the limit, send only the top ones by the
  priority above and leave the rest for a later turn.
- Respond with a single JSON object matching the given schema. No prose, no
  markdown fences, no extra fields.`

// OllamaPlanner drives Planner.Plan with a chat-completions llm.Client under
// a JSON schema. It never calls the http tool itself; everything it proposes
// still passes through model.ValidateAction and Policy before execution.
type OllamaPlanner struct {
	Client llm.Client
	seq    atomic.Int64
}

// schemaForLimit tightens the static schema's "candidates" array to at most
// limit items. The static file's maxItems (5) is only a ceiling; the real,
// current cap is the run's actual candidate budget. Without this, a model
// may legally emit more candidates than the agent will accept, lose the
// whole batch to CodeTooMany, and (at temperature 0) repeat the exact same
// mistake on the one allowed retry.
func schemaForLimit(limit int) []byte {
	if limit <= 0 {
		return outputSchema
	}
	var doc map[string]any
	if err := json.Unmarshal(outputSchema, &doc); err != nil {
		return outputSchema
	}
	props, _ := doc["properties"].(map[string]any)
	cands, _ := props["candidates"].(map[string]any)
	if cands == nil {
		return outputSchema
	}
	if ceiling, ok := cands["maxItems"].(float64); !ok || float64(limit) < ceiling {
		cands["maxItems"] = limit
	}
	patched, err := json.Marshal(doc)
	if err != nil {
		return outputSchema
	}
	return patched
}

type rawOutput struct {
	Candidates          []model.Action       `json:"candidates"`
	HypothesisProposals []HypothesisProposal `json:"hypothesis_proposals"`
}

func (p *OllamaPlanner) Plan(ctx context.Context, in Input) (Output, error) {
	resp, err := p.Client.Generate(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: renderPlannerInput(in)},
		},
		Schema: schemaForLimit(in.Remaining.MaxCandidates),
	})
	if err != nil {
		return Output{}, err
	}
	raw, err := model.DecodeStrict[rawOutput]([]byte(stripFences(resp.Content)))
	if err != nil {
		return Output{}, err
	}
	for i := range raw.Candidates {
		raw.Candidates[i].ID = fmt.Sprintf("P-%d-%d", p.seq.Add(1), i+1)
	}
	return Output{Candidates: raw.Candidates, Proposals: raw.HypothesisProposals}, nil
}

type obsView struct {
	ID          string   `json:"id"`
	Account     string   `json:"account_ref,omitempty"`
	URL         string   `json:"url,omitempty"`
	Status      string   `json:"result_status"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
	ExposedRefs []string `json:"exposed_refs,omitempty"`
	Summary     string   `json:"summary,omitempty"`
}

type hypView struct {
	ID     string `json:"id"`
	Claim  string `json:"claim"`
	Status string `json:"status"`
}

type verificationView struct {
	HypothesisID string   `json:"hypothesis_id"`
	Outcome      string   `json:"outcome"`
	Reasons      []string `json:"reasons,omitempty"`
}

func newObsView(o model.Observation) obsView {
	return obsView{
		ID: o.ID, Account: string(o.AccountRef), URL: o.URL, Status: string(o.ResultStatus),
		EvidenceIDs: o.EvidenceIDs, ExposedRefs: o.ExposedRefs, Summary: o.Summary,
	}
}

func renderPlannerInput(in Input) string {
	obs := make([]obsView, len(in.Observations))
	for i, o := range in.Observations {
		obs[i] = newObsView(o)
	}
	hyps := make([]hypView, len(in.Hypotheses))
	for i, h := range in.Hypotheses {
		hyps[i] = hypView{ID: h.ID, Claim: h.Claim, Status: string(h.Status)}
	}
	vers := make([]verificationView, len(in.Verifications))
	for i, v := range in.Verifications {
		vers[i] = verificationView{HypothesisID: v.HypothesisID, Outcome: string(v.Outcome), Reasons: v.Reasons}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Goal: %s\n\n", in.Goal)
	fmt.Fprintf(&b, "Remaining budget: %d cycles, %d http requests, %d model calls. Max candidates this turn: %d.\n\n",
		in.Remaining.MaxCycles, in.Remaining.MaxHTTPRequests, in.Remaining.MaxModelCalls, in.Remaining.MaxCandidates)
	writeJSON(&b, "Hypotheses", hyps)
	writeJSON(&b, "What the verifier is still missing, in its own words -- propose candidates that address the most recent, still-open reason for the relevant hypothesis_id before anything else", vers)
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
// instructions not to. Validation still rejects anything that isn't then
// well-formed JSON matching the contract.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 && !strings.Contains(s[:nl], "{") {
		s = s[nl+1:] // drop a language tag line, e.g. "json"
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

var _ Planner = (*OllamaPlanner)(nil)
