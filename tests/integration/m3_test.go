// M3 acceptance (AC-01, 02, 03 paths, 08, 09, 10): the agent loop with fake
// Planner/Judge, no model. The evaluator reads the lab answer key; the agent
// and verifier never do.
package integration

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"gosumdochi/internal/agent"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/labapp"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
	"gosumdochi/internal/state"
)

type plannerFunc func(planner.Input) (planner.Output, error)

func (f plannerFunc) Plan(_ context.Context, in planner.Input) (planner.Output, error) { return f(in) }

type judgeFunc func(judge.Input) (model.Decision, error)

func (f judgeFunc) Judge(_ context.Context, in judge.Input) (model.Decision, error) { return f(in) }

func (e *env) runAgent(ctx context.Context, p planner.Planner, j judge.Judge) state.Snapshot {
	e.t.Helper()
	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-1", Goal: "check cross-account document access", ModelID: "fake",
		Planner: p, Judge: j, Exec: e.exec, Store: e.store, Policy: e.pol, Budget: e.budget,
	})
	snap, err := a.Run(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	// The final state must be on disk, readable without the live run.
	disk, err := e.store.LoadState(context.Background())
	if err != nil || disk.Run.StopReason != snap.Run.StopReason || disk.Run.Status != snap.Run.Status {
		e.t.Fatalf("stored state differs: %v %+v", err, disk.Run)
	}
	return snap
}

// confirmed checks only the primary (doc-cross-read-v1) hypothesis: these
// M3 tests are specifically about that rule's AC-01/02/08/09/10 paths. M6a's
// independent side rules (vertical-privesc-v1 etc.) may confirm or reject on
// their own and must not affect these assertions; see m6a_test.go for them.
func confirmed(s state.Snapshot) bool {
	for _, v := range s.Verifications {
		if v.HypothesisID == agent.PrimaryHypothesis && v.Outcome == model.OutcomeConfirmed {
			return true
		}
	}
	return hypothesis(s, agent.PrimaryHypothesis).Status == model.HypConfirmed
}

func hypothesis(s state.Snapshot, id string) model.Hypothesis {
	for _, h := range s.Hypotheses {
		if h.ID == id {
			return h
		}
	}
	return model.Hypothesis{}
}

func lastVerificationFor(s state.Snapshot, hypID string) model.Verification {
	for i := len(s.Verifications) - 1; i >= 0; i-- {
		if s.Verifications[i].HypothesisID == hypID {
			return s.Verifications[i]
		}
	}
	return model.Verification{}
}

func finding(s state.Snapshot, hypID string) (model.Finding, bool) {
	for _, f := range s.Findings {
		if f.HypothesisID == hypID {
			return f, true
		}
	}
	return model.Finding{}, false
}

// AC-01
func TestAgentConfirmsInVulnerableMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if s.Run.Status != model.RunCompleted || s.Run.StopReason != model.StopGoalVerified {
		t.Fatalf("run %+v", s.Run)
	}
	if hypothesis(s, agent.PrimaryHypothesis).Status != model.HypConfirmed {
		t.Fatalf("hypothesis %+v", hypothesis(s, agent.PrimaryHypothesis))
	}
	f, ok := finding(s, agent.PrimaryHypothesis)
	if !ok {
		t.Fatalf("no finding for %s among %d: %+v", agent.PrimaryHypothesis, len(s.Findings), s.Findings)
	}
	var v model.Verification
	for _, x := range s.Verifications {
		if x.ID == f.VerificationID {
			v = x
		}
	}
	t.Logf("confirmed after %d observations, %d decisions, %d http requests; baseline=%v comparison=%v",
		len(s.Observations), len(s.Decisions), e.budget.Used(), v.BaselineIDs, v.ComparisonIDs)
	if v.Outcome != model.OutcomeConfirmed || len(v.BaselineIDs) < 4 || len(v.ComparisonIDs) < 2 {
		t.Fatalf("verification %+v", v)
	}
	// Every referenced evidence ID exists on disk.
	for _, id := range f.EvidenceIDs {
		if _, err := e.store.GetEvidence(context.Background(), id); err != nil {
			t.Errorf("evidence %s: %v", id, err)
		}
	}
	// Evaluator-only check against the answer key: the compared marker really
	// belongs to a document owned by B.
	ans := e.app.Answer()
	cmp, _ := e.store.GetEvidence(context.Background(), v.ComparisonIDs[0])
	found := false
	for _, d := range ans.Docs {
		if d.OwnerID == "u-b" && strings.Contains(cmp.SanitizedResponse, d.Marker) {
			found = true
		}
	}
	if !found || !ans.CrossAccessExpected {
		t.Fatal("confirmation does not match the answer key")
	}
	// Decisions are recorded with IDs, and the model never held "confirmed".
	if len(s.Decisions) == 0 || s.Decisions[0].ID == "" {
		t.Fatal("decisions not recorded")
	}
}

// AC-02
func TestAgentDoesNotConfirmInFixedMode(t *testing.T) {
	e := newEnv(t, opts{mode: labapp.ModeFixed})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if confirmed(s) {
		t.Fatalf("false confirmed: %+v", s.Verifications)
	}
	if s.Run.Status != model.RunCompleted || hypothesis(s, agent.PrimaryHypothesis).Status != model.HypRejected {
		t.Fatalf("run %+v hyp %+v", s.Run, hypothesis(s, agent.PrimaryHypothesis))
	}
	if e.app.Answer().CrossAccessExpected {
		t.Fatal("test setup: fixed mode should not expect cross access")
	}
}

// AC-03: a missing comparison is never confirmed, and the cause is recorded.
func TestAgentLoginFailureIsNotConfirmed(t *testing.T) {
	e := newEnv(t, opts{passB: "wrong"})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if confirmed(s) || s.Run.StopReason != model.StopToolError || s.Run.Status != model.RunIncomplete {
		t.Fatalf("run %+v", s.Run)
	}
	if len(s.Verifications) == 0 || !strings.Contains(strings.Join(s.Verifications[len(s.Verifications)-1].Reasons, " "), "login failed for account B") {
		t.Fatalf("cause not recorded: %+v", s.Verifications)
	}
	if hypothesis(s, agent.PrimaryHypothesis).Status == model.HypConfirmed {
		t.Fatal("hypothesis confirmed")
	}
}

func TestAgentSameAccountTwiceIsNotConfirmed(t *testing.T) {
	e := newEnv(t, opts{sameAcct: true})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if confirmed(s) || s.Run.StopReason != model.StopToolError {
		t.Fatalf("run %+v", s.Run)
	}
	if len(s.Verifications) != 1 || !strings.Contains(s.Verifications[0].Reasons[0], "same identity") {
		t.Fatalf("%+v", s.Verifications)
	}
	// Stopped right after bootstrap: no model call, no document traffic.
	if len(s.Decisions) != 0 {
		t.Fatal("loop ran with identical accounts")
	}
}

func TestAgentReloginFailureIsNotConfirmed(t *testing.T) {
	var logins, docs atomic.Int64
	e := newEnv(t, opts{handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/login" && r.Method == "POST":
				if logins.Add(1) > 2 { // A and B log in; any relogin is rejected
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
			case r.URL.Path == "/api/docs" && docs.Add(1) == 1:
				w.WriteHeader(http.StatusUnauthorized) // session "expired"
				return
			}
			next.ServeHTTP(w, r)
		})
	}})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if confirmed(s) || s.Run.StopReason != model.StopToolError {
		t.Fatalf("run %+v", s.Run)
	}
}

// AC-08
func TestJudgeUnknownCandidateStopsAfterBoundedRecovery(t *testing.T) {
	e := newEnv(t, opts{})
	var calls atomic.Int64
	bad := judgeFunc(func(judge.Input) (model.Decision, error) {
		calls.Add(1)
		return model.Decision{Decision: model.DecisionExecute, SelectedActionID: "no-such-id", ReasonCode: model.ReasonNewObservationNeeded, EvidenceIDs: []string{}}, nil
	})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, bad)
	if s.Run.StopReason != model.StopModelError || s.Run.Status != model.RunIncomplete || confirmed(s) {
		t.Fatalf("run %+v", s.Run)
	}
	if calls.Load() != agent.MaxRecoveries+1 {
		t.Fatalf("judge called %d times", calls.Load())
	}
	if len(s.Decisions) != 0 {
		t.Fatal("invalid decision stored as a decision")
	}
	// Only the three bootstrap requests ever reached the app (plus 2 logins).
	if e.hits.Load() != 5 {
		t.Fatalf("hits %d", e.hits.Load())
	}
}

func TestJudgeRecoversOnce(t *testing.T) {
	e := newEnv(t, opts{})
	first := true
	flaky := judgeFunc(func(in judge.Input) (model.Decision, error) {
		if first {
			first = false
			return model.Decision{Decision: "delete_everything", ReasonCode: model.ReasonNewObservationNeeded}, nil
		}
		return judge.FirstCandidate{}.Judge(context.Background(), in)
	})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, flaky)
	if s.Run.StopReason != model.StopGoalVerified {
		t.Fatalf("run %+v", s.Run)
	}
}

func TestJudgeCannotStopAsVerifiedWithoutConfirmation(t *testing.T) {
	e := newEnv(t, opts{})
	liar := judgeFunc(func(judge.Input) (model.Decision, error) {
		return model.Decision{Decision: model.DecisionStop, ReasonCode: model.ReasonHypothesisVerified, EvidenceIDs: []string{}}, nil
	})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, liar)
	if confirmed(s) || s.Run.StopReason != model.StopModelError {
		t.Fatalf("run %+v", s.Run)
	}
}

func TestPlannerInventedURLNeverReachesNetwork(t *testing.T) {
	e := newEnv(t, opts{})
	invented := plannerFunc(func(in planner.Input) (planner.Output, error) {
		src := in.Observations[len(in.Observations)-1]
		return planner.Output{Candidates: []model.Action{{
			ID: "X1", Tool: model.ToolHTTPRequest, HypothesisID: agent.PrimaryHypothesis,
			TypedArgs: model.HTTPArgs{Method: "GET", URL: e.url("/api/docs/d-guessed"), AccountRef: "A", SourceObservationID: src.ID},
		}, {
			ID: "X2", Tool: model.ToolHTTPRequest, HypothesisID: agent.PrimaryHypothesis,
			TypedArgs: model.HTTPArgs{Method: "GET", URL: "http://host.docker.internal:11434/api/tags", AccountRef: "A", SourceObservationID: src.ID},
		}}}, nil
	})
	s := e.runAgent(context.Background(), invented, judge.FirstCandidate{})
	if s.Run.StopReason != model.StopModelError || e.hits.Load() != 5 {
		t.Fatalf("run %+v hits %d", s.Run, e.hits.Load())
	}
}

// AC-09: matching status codes without ownership evidence stay inconclusive.
func TestStatusOnlyIsInconclusive(t *testing.T) {
	e := newEnv(t, opts{handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/docs/") {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"message":"ok"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}})
	s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
	if confirmed(s) {
		t.Fatalf("confirmed from status only: %+v", s.Verifications)
	}
	if h := hypothesis(s, agent.PrimaryHypothesis); h.Status != model.HypInconclusive {
		t.Fatalf("hypothesis %+v", h)
	}
	last := lastVerificationFor(s, agent.PrimaryHypothesis)
	if last.Outcome != model.OutcomeInconclusive || len(last.Reasons) == 0 {
		t.Fatalf("%+v", last)
	}
}

// AC-10
func TestRepeatedActionStops(t *testing.T) {
	e := newEnv(t, opts{})
	var repeatedURL string
	same := plannerFunc(func(in planner.Input) (planner.Output, error) {
		src := in.Observations[0] // start page, exposes a fixed set of links
		repeatedURL = src.ExposedRefs[0]
		return planner.Output{Candidates: []model.Action{{
			ID: "R" + itoa(len(in.Observations)), Tool: model.ToolHTTPRequest, HypothesisID: agent.PrimaryHypothesis,
			TypedArgs: model.HTTPArgs{Method: "GET", URL: repeatedURL, AccountRef: "A", SourceObservationID: src.ID},
		}}}, nil
	})
	s := e.runAgent(context.Background(), same, judge.FirstCandidate{})
	if s.Run.StopReason != model.StopRepeatedAction || s.Run.Status != model.RunIncomplete {
		t.Fatalf("run %+v", s.Run)
	}
	n := 0
	for _, o := range s.Observations {
		if o.URL == repeatedURL {
			n++
		}
	}
	if n != agent.MaxExecutionsPerRequest {
		t.Fatalf("executed %d times", n)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestBudgetsStopTheLoop(t *testing.T) {
	cases := map[string]struct {
		budgets func(*model.Budgets)
	}{
		"http":   {func(b *model.Budgets) { b.MaxHTTPRequests = 7 }},
		"model":  {func(b *model.Budgets) { b.MaxModelCalls = 3 }},
		"cycles": {func(b *model.Budgets) { b.MaxCycles = 2 }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, opts{budgets: c.budgets})
			s := e.runAgent(context.Background(), planner.LabHeuristic{}, judge.FirstCandidate{})
			if s.Run.StopReason != model.StopBudgetExhausted || s.Run.Status != model.RunIncomplete || confirmed(s) {
				t.Fatalf("run %+v", s.Run)
			}
			if e.budget.Used() > e.cfg.Budgets.MaxHTTPRequests || e.hits.Load() > int64(e.cfg.Budgets.MaxHTTPRequests) {
				t.Fatalf("over budget: used %d hits %d", e.budget.Used(), e.hits.Load())
			}
		})
	}
}

func TestCancelLeavesStoppedRunRecord(t *testing.T) {
	e := newEnv(t, opts{})
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	p := plannerFunc(func(in planner.Input) (planner.Output, error) {
		if n++; n == 2 {
			cancel() // Ctrl+C during the second planning step
		}
		return planner.LabHeuristic{}.Plan(ctx, in)
	})
	s := e.runAgent(ctx, p, judge.FirstCandidate{})
	if s.Run.StopReason != model.StopUserCanceled || s.Run.Status != model.RunCanceled || confirmed(s) {
		t.Fatalf("run %+v", s.Run)
	}
	if len(s.Observations) == 0 {
		t.Fatal("collected observations must be kept")
	}
}

func TestExtraHypothesesStayUnconfirmed(t *testing.T) {
	e := newEnv(t, opts{})
	base := planner.LabHeuristic{}
	p := plannerFunc(func(in planner.Input) (planner.Output, error) {
		out, err := base.Plan(context.Background(), in)
		out.Proposals = []planner.HypothesisProposal{{ID: "H-2", Claim: "listing leaks other users"}}
		return out, err
	})
	s := e.runAgent(context.Background(), p, judge.FirstCandidate{})
	if h := hypothesis(s, "H-2"); h.ID == "" || h.Status == model.HypConfirmed {
		t.Fatalf("H-2 %+v", h)
	}
	if hypothesis(s, "H-2").Status != model.HypInconclusive {
		t.Fatalf("unverified hypothesis must end inconclusive: %+v", hypothesis(s, "H-2"))
	}
}
