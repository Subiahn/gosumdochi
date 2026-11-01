// Package agent runs the decision loop: bootstrap observations, then cycles of
// plan → filter → judge → policy+execute → verify, until a stop condition.
// Models only propose and select; scope, budgets, verification and the
// confirmed status are decided by code.
package agent

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"gosumdochi/internal/config"
	"gosumdochi/internal/executor"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
	"gosumdochi/internal/policy"
	"gosumdochi/internal/state"
	"gosumdochi/internal/tools"
	"gosumdochi/internal/verifier"
)

const (
	// MaxRecoveries is the number of extra attempts after an invalid model
	// output within one planner or judge call.
	MaxRecoveries = 1
	// MaxExecutionsPerRequest allows one original run plus one verification re-run.
	MaxExecutionsPerRequest = 2
	// MaxHypotheses bounds model-proposed hypotheses.
	MaxHypotheses = 5
	// ContextObservations is how many recent observations the models see.
	ContextObservations = 12

	// PrimaryHypothesis is seeded by the system; the verifier rule applies to it.
	PrimaryHypothesis = "H-1"
	primaryClaim      = "One test account can read a document owned by the other test account (missing object-level access control)."

	// M6a side hypotheses: seeded like PrimaryHypothesis, checked every cycle,
	// but confirming or rejecting one of these never stops the run -- only
	// PrimaryHypothesis drives the overall stop condition.
	HypPrivesc   = "H-PRIVESC"
	privescClaim = "An account without the admin role can reach an admin-only endpoint's data."
	HypUnauth    = "H-UNAUTH"
	unauthClaim  = "A resource that should require login is reachable with no session at all."
	HypPassive   = "H-PASSIVE"
	passiveClaim = "A response exposes internal information (debug header or info-disclosure pattern)."
)

type Deps struct {
	Config  config.Config
	RunID   string
	Goal    string
	ModelID string
	// ModelOptions is recorded in run metadata as given (FR-12); it is never
	// sent anywhere by the agent itself.
	ModelOptions map[string]any
	Planner      planner.Planner
	Judge        judge.Judge
	Exec         *executor.Executor
	Store        state.Store
	Policy       *policy.Policy
	Budget       *policy.Budget // HTTP request budget shared with the tool
	Now          func() time.Time
}

type Agent struct {
	d Deps

	parent context.Context // caller context: its cancellation is a user cancel
	run    model.Run
	snap   state.Snapshot
	cycle  int
	models int

	records          []verifier.Record
	adminRecords     []verifier.AdminRecord
	rawEvidence      []model.Evidence
	publicDocChecked map[string]bool
	evidence         map[string]struct{}
	obsByID          map[string]model.Observation
	hypByID          map[string]int // index in snap.Hypotheses
	actionID         map[string]bool
	execs            map[string]int // request key -> executions
	seq              struct{ dec, ver, find int }
}

// stop ends the loop with a reason.
type stop struct {
	reason model.StopReason
	note   string
}

func (s stop) Error() string { return string(s.reason) + ": " + s.note }

func New(d Deps) *Agent {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Agent{
		d: d, evidence: map[string]struct{}{}, obsByID: map[string]model.Observation{},
		hypByID: map[string]int{}, actionID: map[string]bool{}, execs: map[string]int{},
		publicDocChecked: map[string]bool{},
	}
}

func (a *Agent) meta() model.Meta {
	return model.Meta{SchemaVersion: model.SchemaVersion, RunID: a.d.RunID, Timestamp: a.d.Now().UTC()}
}

func (a *Agent) event(ctx context.Context, kind string, payload any) {
	// A failed event write is an infrastructure failure, surfaced at the next save.
	if err := a.d.Store.AppendEvent(ctx, kind, payload); err != nil {
		panic(stop{model.StopInfrastructureError, "event write: " + err.Error()})
	}
}

// Run executes the loop. The returned snapshot is always the final stored
// state; the error is non-nil only for infrastructure failures.
func (a *Agent) Run(parent context.Context) (snap state.Snapshot, err error) {
	b := *a.d.Config.Budgets
	ctx, cancel := context.WithTimeout(parent, time.Duration(b.TotalTimeoutSec)*time.Second)
	defer cancel()

	a.run = model.Run{
		Meta: a.meta(), ID: a.d.RunID, Goal: a.d.Goal, ConfigDigest: a.d.Config.Digest(),
		ModelID: a.d.ModelID, ModelOptions: a.d.ModelOptions, Status: model.RunRunning, Budgets: b,
		Versions: map[string]string{
			"tool_contract": "http_request/1", "decision_contract": "1", "verifier_rule": verifier.RuleID,
			"planner_output_schema": "1", "judge_decision_schema": "1",
		},
	}
	a.snap = state.Snapshot{Meta: a.meta()}
	a.addHypothesis(PrimaryHypothesis, primaryClaim)
	a.addHypothesis(HypPrivesc, privescClaim)
	a.addHypothesis(HypUnauth, unauthClaim)
	a.addHypothesis(HypPassive, passiveClaim)

	a.parent = parent
	var st stop
	func() {
		defer func() {
			if r := recover(); r != nil {
				s, ok := r.(stop)
				if !ok {
					panic(r)
				}
				st = s
			}
		}()
		a.event(ctx, "run_start", a.run)
		st = a.loop(ctx)
	}()

	return a.finish(parent, st)
}

func (a *Agent) finish(parent context.Context, st stop) (state.Snapshot, error) {
	ctx := context.WithoutCancel(parent) // the final record must survive Ctrl+C
	for i := range a.snap.Hypotheses {
		h := &a.snap.Hypotheses[i]
		if h.Status == model.HypProposed || h.Status == model.HypTesting {
			if h.Status == model.HypProposed {
				h.Status = model.HypTesting
			}
			h.Status = model.HypInconclusive
		}
	}
	a.run.StopReason, a.run.StopNote = st.reason, st.note
	a.run.Cycles, a.run.ModelCalls, a.run.HTTPRequests = a.cycle, a.models, a.d.Budget.Used()
	switch st.reason {
	case model.StopGoalVerified, model.StopExplorationExhaust:
		a.run.Status = model.RunCompleted
	case model.StopUserCanceled:
		a.run.Status = model.RunCanceled
	case model.StopInfrastructureError:
		a.run.Status = model.RunFailed
	default:
		a.run.Status = model.RunIncomplete
	}
	a.run.Timestamp = a.d.Now().UTC()
	a.snap.Run = a.run
	a.snap.Timestamp = a.run.Timestamp
	var err error
	if serr := a.d.Store.AppendEvent(ctx, "run_end", map[string]any{"status": a.run.Status, "stop_reason": st.reason, "note": st.note, "model_calls": a.models, "cycles": a.cycle}); serr != nil {
		err = serr
	}
	if serr := a.d.Store.SaveState(ctx, a.snap); serr != nil {
		err = serr
	}
	return a.snap, err
}

func (a *Agent) save(ctx context.Context) {
	a.snap.Run = a.run
	a.snap.Timestamp = a.d.Now().UTC()
	if err := a.d.Store.SaveState(ctx, a.snap); err != nil {
		panic(stop{model.StopInfrastructureError, "save state: " + err.Error()})
	}
}

func (a *Agent) loop(ctx context.Context) stop {
	if st := a.bootstrap(ctx); st != nil {
		return *st
	}
	a.setStatus(PrimaryHypothesis, model.HypTesting)
	a.setStatus(HypPrivesc, model.HypTesting)
	a.setStatus(HypUnauth, model.HypTesting)
	a.setStatus(HypPassive, model.HypTesting)
	a.save(ctx)

	for a.cycle = 1; ; a.cycle++ {
		if st := a.checkLimits(ctx); st != nil {
			return *st
		}
		cands, st := a.callPlanner(ctx)
		if st != nil {
			return *st
		}
		if len(cands) == 0 {
			return stop{model.StopExplorationExhaust, "planner offered no further candidates"}
		}
		dec, st := a.callJudge(ctx, cands)
		if st != nil {
			return *st
		}
		switch dec.Decision {
		case model.DecisionStop, model.DecisionInconclusive:
			return stop{model.StopExplorationExhaust, "judge: " + string(dec.ReasonCode)}
		}
		var chosen model.Action
		for _, c := range cands {
			if c.ID == dec.SelectedActionID {
				chosen = c
			}
		}
		obs, st := a.execute(ctx, chosen)
		if st != nil {
			return *st
		}
		_ = obs
		if st := a.verify(ctx); st != nil {
			return *st
		}
		a.save(ctx)
	}
}

func (a *Agent) checkLimits(ctx context.Context) *stop {
	switch {
	case a.parent.Err() != nil:
		return &stop{model.StopUserCanceled, "canceled"}
	case ctx.Err() != nil:
		return &stop{model.StopBudgetExhausted, "total timeout"}
	case a.cycle > a.run.Budgets.MaxCycles:
		return &stop{model.StopBudgetExhausted, "cycle limit"}
	case a.d.Budget.Remaining() <= 0:
		return &stop{model.StopBudgetExhausted, "http request budget"}
	}
	return nil
}

// ---- bootstrap -----------------------------------------------------------

// bootstrap observes the start page and both identities using paths from the
// configuration. These actions are not model output.
func (a *Agent) bootstrap(ctx context.Context) *stop {
	cfg := a.d.Config
	origin := strings.TrimRight(cfg.TargetOrigin, "/")
	steps := []model.Action{
		{ID: "boot-1", TypedArgs: model.HTTPArgs{URL: origin + cfg.StartPath, AccountRef: model.AccountA}},
		{ID: "boot-2", TypedArgs: model.HTTPArgs{URL: origin + cfg.Accounts[model.AccountA].IdentityPath, AccountRef: model.AccountA}},
		{ID: "boot-3", TypedArgs: model.HTTPArgs{URL: origin + cfg.Accounts[model.AccountB].IdentityPath, AccountRef: model.AccountB}},
	}
	for _, s := range steps {
		s.Tool = model.ToolHTTPRequest
		s.HypothesisID = PrimaryHypothesis
		s.TypedArgs.Method = "GET"
		s.TypedArgs.SourceObservationID = "bootstrap"
		s.RationaleSummary = "bootstrap observation from configuration"
		if st := a.checkLimits(ctx); st != nil {
			return st
		}
		if _, st := a.execute(ctx, s); st != nil {
			return st
		}
	}
	// The comparison needs two working, distinct accounts (AC-03).
	return a.verify(ctx)
}

// ---- planner -------------------------------------------------------------

func (a *Agent) remaining() model.Budgets {
	b := a.run.Budgets
	b.MaxCycles = max(0, b.MaxCycles-a.cycle+1)
	b.MaxHTTPRequests = a.d.Budget.Remaining()
	b.MaxModelCalls = max(0, b.MaxModelCalls-a.models)
	return b
}

func (a *Agent) recent() []model.Observation {
	o := a.snap.Observations
	if len(o) > ContextObservations {
		o = o[len(o)-ContextObservations:]
	}
	return append([]model.Observation(nil), o...)
}

// llmCtx bounds one model call at Budgets.LLMTimeoutSec, in addition to the
// run's overall deadline already carried by ctx.
func (a *Agent) llmCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, time.Duration(a.run.Budgets.LLMTimeoutSec)*time.Second)
}

func truncateErr(err error) string {
	s := err.Error()
	const max = 300
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func (a *Agent) spendModel() *stop {
	if a.models >= a.run.Budgets.MaxModelCalls {
		return &stop{model.StopBudgetExhausted, "model call budget"}
	}
	a.models++
	return nil
}

func (a *Agent) callPlanner(ctx context.Context) ([]model.Action, *stop) {
	feedback := ""
	for attempt := 0; attempt <= MaxRecoveries; attempt++ {
		if st := a.spendModel(); st != nil {
			return nil, st
		}
		callCtx, cancel := a.llmCtx(ctx)
		out, err := a.d.Planner.Plan(callCtx, planner.Input{
			Goal: a.d.Goal, Observations: a.recent(), Hypotheses: append([]model.Hypothesis(nil), a.snap.Hypotheses...),
			Verifications: append([]model.Verification(nil), a.snap.Verifications...),
			Remaining:     a.remaining(), Feedback: feedback,
		})
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil, &stop{model.StopBudgetExhausted, "interrupted during planner call"}
			}
			feedback = "planner_error"
			a.event(ctx, "planner_error", map[string]any{"cycle": a.cycle, "attempt": attempt, "error": truncateErr(err)})
			continue
		}
		a.registerProposals(ctx, out.Proposals)
		cands, invalid, repeated, code := a.filter(ctx, out.Candidates)
		a.event(ctx, "plan", map[string]any{"cycle": a.cycle, "proposed": len(out.Candidates), "accepted": len(cands), "invalid": invalid, "repeated": repeated})
		switch {
		case len(cands) > 0:
			return cands, nil
		case len(out.Candidates) == 0:
			return nil, nil
		case invalid == 0 && repeated > 0:
			return nil, &stop{model.StopRepeatedAction, "all candidates repeat earlier requests"}
		}
		feedback = string(code)
	}
	return nil, &stop{model.StopModelError, "planner output invalid after recovery"}
}

func (a *Agent) refs() model.Refs {
	r := model.Refs{
		Evidence: a.evidence, Observations: a.obsByID, Hypotheses: map[string]struct{}{}, Candidates: map[string]model.Action{},
	}
	for id := range a.hypByID {
		r.Hypotheses[id] = struct{}{}
	}
	return r
}

// filter drops invalid, out-of-scope and repeated candidates. Nothing it
// drops reaches the judge or the network.
func (a *Agent) filter(ctx context.Context, in []model.Action) (ok []model.Action, invalid, repeated int, code model.ErrorCode) {
	limit := a.run.Budgets.MaxCandidates
	if len(in) > limit {
		a.event(ctx, "candidate_rejected", map[string]any{"code": model.CodeTooMany})
		return nil, len(in), 0, model.CodeTooMany
	}
	refs := a.refs()
	seen := map[string]bool{}
	for _, c := range in {
		err := model.ValidateAction(c, refs)
		if err == nil && (a.actionID[c.ID] || seen[c.ID]) {
			err = &model.ValidationError{Code: model.CodeInvalidValue, Field: "id", Msg: "duplicate id"}
		}
		if err == nil {
			err = a.d.Policy.CheckAction(c)
		}
		if err != nil {
			invalid++
			code = model.CodeOf(err)
			if code == "" {
				code = model.ErrorCode("policy_" + policy.DeniedCode(err))
			}
			a.event(ctx, "candidate_rejected", map[string]any{"id": c.ID, "code": code})
			continue
		}
		if a.execs[requestKey(c)] >= MaxExecutionsPerRequest {
			repeated++
			a.event(ctx, "candidate_rejected", map[string]any{"id": c.ID, "code": "repeated_action"})
			continue
		}
		seen[c.ID] = true
		ok = append(ok, c)
	}
	return ok, invalid, repeated, code
}

func (a *Agent) registerProposals(ctx context.Context, ps []planner.HypothesisProposal) {
	for _, p := range ps {
		if p.ID == "" || strings.TrimSpace(p.Claim) == "" || len(a.hypByID) >= MaxHypotheses {
			continue
		}
		if _, dup := a.hypByID[p.ID]; dup {
			continue
		}
		a.addHypothesis(p.ID, p.Claim)
		a.event(ctx, "hypothesis_proposed", map[string]string{"id": p.ID})
	}
}

func (a *Agent) addHypothesis(id, claim string) {
	a.hypByID[id] = len(a.snap.Hypotheses)
	a.snap.Hypotheses = append(a.snap.Hypotheses, model.Hypothesis{
		Meta: a.meta(), ID: id, Claim: claim, Status: model.HypProposed,
		SupportingEvidenceIDs: []string{}, ContradictingEvidenceIDs: []string{},
	})
}

func (a *Agent) setStatus(id string, to model.HypothesisStatus) {
	h := &a.snap.Hypotheses[a.hypByID[id]]
	if h.Status == to {
		return
	}
	if !model.CanTransition(h.Status, to) {
		panic(stop{model.StopInfrastructureError, fmt.Sprintf("illegal hypothesis transition %s -> %s", h.Status, to)})
	}
	h.Status = to
	h.Timestamp = a.d.Now().UTC()
}

// ---- judge ---------------------------------------------------------------

func (a *Agent) callJudge(ctx context.Context, cands []model.Action) (model.Decision, *stop) {
	feedback := ""
	refs := a.refs()
	for _, c := range cands {
		refs.Candidates[c.ID] = c
	}
	for attempt := 0; attempt <= MaxRecoveries; attempt++ {
		if st := a.spendModel(); st != nil {
			return model.Decision{}, st
		}
		callCtx, cancel := a.llmCtx(ctx)
		d, err := a.d.Judge.Judge(callCtx, judge.Input{
			Candidates: cands, Hypotheses: append([]model.Hypothesis(nil), a.snap.Hypotheses...), Observations: a.recent(),
			Verifications: append([]model.Verification(nil), a.snap.Verifications...), Remaining: a.remaining(), Feedback: feedback,
		})
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return d, &stop{model.StopBudgetExhausted, "interrupted during judge call"}
			}
			feedback = "judge_error"
			a.event(ctx, "judge_error", map[string]any{"cycle": a.cycle, "attempt": attempt, "error": truncateErr(err)})
			continue
		}
		verr := model.ValidateDecision(d, refs)
		if verr == nil && d.Decision == model.DecisionStop && d.ReasonCode == model.ReasonHypothesisVerified && !a.hasConfirmed() {
			verr = &model.ValidationError{Code: model.CodeInvalidValue, Field: "reason_code", Msg: "hypothesis_verified without a confirmed verification"}
		}
		if verr != nil {
			feedback = string(model.CodeOf(verr))
			a.event(ctx, "decision_rejected", map[string]any{"cycle": a.cycle, "attempt": attempt, "code": feedback})
			continue
		}
		a.seq.dec++
		d.ID, d.Meta = fmt.Sprintf("D-%04d", a.seq.dec), a.meta()
		a.snap.Decisions = append(a.snap.Decisions, d)
		a.event(ctx, "decision", d)
		return d, nil
	}
	return model.Decision{}, &stop{model.StopModelError, "judge output invalid after recovery"}
}

func (a *Agent) hasConfirmed() bool {
	for _, v := range a.snap.Verifications {
		if v.Outcome == model.OutcomeConfirmed {
			return true
		}
	}
	return false
}

// ---- execution -----------------------------------------------------------

func requestKey(c model.Action) string {
	u, err := url.Parse(c.TypedArgs.URL)
	if err != nil {
		return c.TypedArgs.URL
	}
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var qs []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		qs = append(qs, k+"="+strings.Join(vs, ","))
	}
	return strings.Join([]string{c.TypedArgs.Method, string(c.TypedArgs.AccountRef), strings.ToLower(u.Scheme + "://" + u.Host), u.Path, strings.Join(qs, "&")}, "|")
}

func (a *Agent) execute(ctx context.Context, act model.Action) (model.Observation, *stop) {
	if a.d.Budget.Remaining() <= 0 {
		return model.Observation{}, &stop{model.StopBudgetExhausted, "http request budget"}
	}
	act.Meta = a.meta()
	a.actionID[act.ID] = true
	a.execs[requestKey(act)]++
	a.snap.Actions = append(a.snap.Actions, act)

	obs, err := a.d.Exec.Run(ctx, act)
	if err != nil {
		return obs, &stop{model.StopInfrastructureError, "execute: " + err.Error()}
	}
	a.snap.Observations = append(a.snap.Observations, obs)
	a.obsByID[obs.ID] = obs
	for _, id := range obs.EvidenceIDs {
		a.evidence[id] = struct{}{}
		ev, err := a.d.Store.GetEvidence(ctx, id)
		if err != nil {
			return obs, &stop{model.StopInfrastructureError, "read evidence: " + err.Error()}
		}
		path := ""
		if u, err := url.Parse(act.TypedArgs.URL); err == nil {
			path = u.Path
		}
		a.records = append(a.records, verifier.NewRecord(ev, act.TypedArgs.URL, path))
		a.rawEvidence = append(a.rawEvidence, ev)
		if path == "/api/admin/users" {
			a.adminRecords = append(a.adminRecords, verifier.NewAdminRecord(ev, path))
		}
	}

	switch {
	case obs.ResultStatus == model.ResultCanceled:
		if a.parent.Err() != nil {
			return obs, &stop{model.StopUserCanceled, "request canceled"}
		}
		return obs, &stop{model.StopBudgetExhausted, "total timeout"}
	case obs.ResultStatus == model.ResultDenied && strings.Contains(obs.Summary, tools.ErrBudgetExhausted):
		return obs, &stop{model.StopBudgetExhausted, "http request budget"}
	case obs.ResultStatus == model.ResultFailed && strings.Contains(obs.Summary, tools.ErrLoginFailed):
		// AC-03: no comparison can be built on a broken account session.
		a.recordVerification(ctx, PrimaryHypothesis, model.Verification{
			Outcome: model.OutcomeInconclusive, RuleID: verifier.RuleID,
			Reasons: []string{fmt.Sprintf("login failed for account %s; comparison not attempted", act.TypedArgs.AccountRef)},
		})
		return obs, &stop{model.StopToolError, "login failed for account " + string(act.TypedArgs.AccountRef)}
	}
	if st := a.maybeCheckUnauth(ctx, act, obs); st != nil {
		return obs, st
	}
	return obs, nil
}

// maybeCheckUnauth is unauth-access-v1's only code-driven step (SDD §14.1):
// when an authenticated read's response exposes a /api/public/docs/{id}
// mirror URL, the agent -- not the Planner -- fetches it once with no
// session at all. act.TypedArgs.AccountRef == AccountNone marks the
// follow-up itself, so it never re-triggers another one.
func (a *Agent) maybeCheckUnauth(ctx context.Context, act model.Action, obs model.Observation) *stop {
	if act.TypedArgs.AccountRef == model.AccountNone {
		return nil
	}
	for _, ref := range obs.ExposedRefs {
		u, err := url.Parse(ref)
		if err != nil || !strings.HasPrefix(u.Path, "/api/public/docs/") || a.publicDocChecked[ref] {
			continue
		}
		a.publicDocChecked[ref] = true
		anon := model.Action{
			ID: fmt.Sprintf("anon-%d", len(a.snap.Actions)+1), Tool: model.ToolHTTPRequest, HypothesisID: HypUnauth,
			TypedArgs:        model.HTTPArgs{Method: "GET", URL: ref, AccountRef: model.AccountNone},
			RationaleSummary: "code-driven unauth-access-v1 check (not model-proposed)",
		}
		if _, st := a.execute(ctx, anon); st != nil {
			return st
		}
	}
	return nil
}

// ---- verification --------------------------------------------------------

// verify runs the primary deterministic rule over all evidence and acts on
// its outcome, then checks the M6a side rules (SDD §14.1), which never
// drive the overall stop condition -- only PrimaryHypothesis does.
func (a *Agent) verify(ctx context.Context) *stop {
	v := verifier.Verify(a.records, a.d.Config.Accounts[model.AccountA].IdentityPath)
	if a.cycle == 0 {
		// Right after bootstrap only one precondition is enforced: the two
		// accounts must be distinct (AC-03). Other gaps are filled by the loop.
		for _, r := range v.Reasons {
			if strings.Contains(r, "same identity") {
				a.recordVerification(ctx, PrimaryHypothesis, v)
				return &stop{model.StopToolError, "accounts resolve to the same identity"}
			}
		}
		return nil
	}
	a.recordVerification(ctx, PrimaryHypothesis, v)
	a.checkSideRules(ctx)
	switch v.Outcome {
	case model.OutcomeConfirmed:
		a.confirm(ctx, PrimaryHypothesis, v,
			"A test account could read another test account's document",
			"In the lab app, an authenticated account retrieved a document owned by a different account.",
			[]string{
				"Rule " + verifier.RuleID + " is specific to this synthetic practice scenario.",
				"Only the observed document was tested; other resources were not assessed.",
				fmt.Sprintf("Based on %d completed requests in this run.", len(a.records)),
			})
		return &stop{model.StopGoalVerified, "verification confirmed"}
	case model.OutcomeRejected:
		a.setStatus(PrimaryHypothesis, model.HypRejected)
		h := &a.snap.Hypotheses[a.hypByID[PrimaryHypothesis]]
		h.ContradictingEvidenceIDs = append([]string(nil), v.ComparisonIDs...)
		return &stop{model.StopExplorationExhaust, "hypothesis rejected by verification"}
	}
	return nil
}

// checkSideRules evaluates the three M6a rules every cycle. Each is
// independent: confirming or rejecting one never stops the run and never
// affects another rule's hypothesis.
func (a *Agent) checkSideRules(ctx context.Context) {
	a.checkSideRule(ctx, HypPrivesc, verifier.VerifyVerticalPrivesc(a.adminRecords, model.AccountA, model.AccountB),
		"A regular account could access the admin-only user list",
		"In the lab app, an account without the admin role retrieved the same data an admin-only endpoint returns to an admin account.",
		[]string{
			"Rule " + verifier.VerticalPrivescRuleID + " is specific to this synthetic practice scenario.",
			"Only the one admin endpoint was tested.",
		})
	a.checkSideRule(ctx, HypUnauth, verifier.VerifyUnauthAccess(a.records),
		"A resource was reachable with no session at all",
		"In the lab app, a request sent without any login session returned data that should require authentication.",
		[]string{
			"Rule " + verifier.UnauthAccessRuleID + " is specific to this synthetic practice scenario.",
			"Only the one endpoint checked was tested.",
		})
	a.checkSideRule(ctx, HypPassive, verifier.VerifyPassiveExposure(a.rawEvidence),
		"Responses exposed internal information",
		"Collected responses contained a debug header or an information-disclosure pattern that should not reach a client.",
		[]string{
			"Rule " + verifier.PassiveExposureRuleID + " is a passive check over evidence already collected; it does not by itself indicate an exploitable vulnerability.",
		})
}

func (a *Agent) checkSideRule(ctx context.Context, hypID string, v model.Verification, title, impact string, limitations []string) {
	a.recordVerification(ctx, hypID, v)
	switch v.Outcome {
	case model.OutcomeConfirmed:
		if a.snap.Hypotheses[a.hypByID[hypID]].Status != model.HypConfirmed {
			a.confirm(ctx, hypID, v, title, impact, limitations)
		}
	case model.OutcomeRejected:
		a.setStatus(hypID, model.HypRejected)
		h := &a.snap.Hypotheses[a.hypByID[hypID]]
		h.ContradictingEvidenceIDs = append([]string(nil), v.ComparisonIDs...)
	}
}

// lastVerificationFor finds the most recent recorded Verification for hypID.
func (a *Agent) lastVerificationFor(hypID string) model.Verification {
	for i := len(a.snap.Verifications) - 1; i >= 0; i-- {
		if a.snap.Verifications[i].HypothesisID == hypID {
			return a.snap.Verifications[i]
		}
	}
	return model.Verification{}
}

// recordVerification appends a verification for hypID, only when its
// content changed from that hypothesis's own last recorded verification.
func (a *Agent) recordVerification(ctx context.Context, hypID string, v model.Verification) {
	if last := a.lastVerificationFor(hypID); last.ID != "" {
		if last.Outcome == v.Outcome && strings.Join(last.Reasons, "|") == strings.Join(v.Reasons, "|") &&
			strings.Join(last.ComparisonIDs, ",") == strings.Join(v.ComparisonIDs, ",") {
			return
		}
	}
	a.seq.ver++
	v.ID, v.Meta, v.HypothesisID = fmt.Sprintf("V-%04d", a.seq.ver), a.meta(), hypID
	if v.BaselineIDs == nil {
		v.BaselineIDs = []string{}
	}
	if v.ComparisonIDs == nil {
		v.ComparisonIDs = []string{}
	}
	a.snap.Verifications = append(a.snap.Verifications, v)
	a.event(ctx, "verification", v)
}

func (a *Agent) confirm(ctx context.Context, hypID string, v model.Verification, title, impact string, limitations []string) {
	a.setStatus(hypID, model.HypConfirmed)
	h := &a.snap.Hypotheses[a.hypByID[hypID]]
	h.SupportingEvidenceIDs = append([]string(nil), v.ComparisonIDs...)

	last := a.lastVerificationFor(hypID)
	a.seq.find++
	f := model.Finding{
		Meta: a.meta(), ID: fmt.Sprintf("F-%04d", a.seq.find), HypothesisID: hypID,
		Title: title, Impact: impact, Severity: model.SeverityForRule[v.RuleID],
		VerificationID: last.ID,
		EvidenceIDs:    append(append([]string{}, v.BaselineIDs...), v.ComparisonIDs...),
		Limitations:    limitations,
	}
	vm := map[string]model.Verification{}
	for _, x := range a.snap.Verifications {
		vm[x.ID] = x
	}
	if err := model.ValidateFinding(f, vm, a.refs()); err != nil {
		panic(stop{model.StopInfrastructureError, "finding invalid: " + err.Error()})
	}
	a.snap.Findings = append(a.snap.Findings, f)
	a.event(ctx, "finding", f)
}
