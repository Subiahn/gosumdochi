// M4 completion check: the real Ollama adapter drives Planner and Judge
// against the lab app, with no fake. Skipped unless OLLAMA_LIVE_MODEL is set
// (see internal/llm/ollama.TestLive for the single-call smoke test this
// builds on).
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"gosumdochi/internal/agent"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/labapp"
	"gosumdochi/internal/llm/ollama"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
)

func liveClient(t *testing.T) (*ollama.Client, string) {
	t.Helper()
	m := os.Getenv("OLLAMA_LIVE_MODEL")
	if m == "" {
		t.Skip("set OLLAMA_LIVE_MODEL (e.g. qwen3:8b) to run against a real Ollama")
	}
	base := os.Getenv("OLLAMA_URL")
	if base == "" {
		base = "http://localhost:11434"
	}
	budgets := model.DefaultBudgets()
	c, err := ollama.New(base, m, time.Duration(budgets.LLMTimeoutSec)*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.ResolveModel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c, id
}

// TestLiveAgentConfirmsInVulnerableMode is the real-model counterpart of
// TestAgentConfirmsInVulnerableMode (AC-01): same lab app, same Verifier
// rule, but Planner and Judge are an actual local model instead of fakes.
func TestLiveAgentConfirmsInVulnerableMode(t *testing.T) {
	client, modelID := liveClient(t)
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})

	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-live-1", Goal: "check cross-account document access",
		ModelID: modelID, ModelOptions: client.Options(),
		Planner: &planner.OllamaPlanner{Client: client}, Judge: judge.OllamaJudge{Client: client},
		Exec: e.exec, Store: e.store, Policy: e.pol, Budget: e.budget,
	})
	snap, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s: stop_reason=%s status=%s cycles=%d decisions=%d http_requests=%d",
		modelID, snap.Run.StopReason, snap.Run.Status, len(snap.Observations), len(snap.Decisions), e.budget.Used())
	for _, d := range snap.Decisions {
		t.Logf("decision %s: %s (%s)", d.ID, d.Decision, d.ReasonCode)
	}

	// The model may fail to reach the goal within budget (recorded, not a
	// test failure by itself) but it must never falsely confirm, and every
	// decision it made must be on record either way.
	if confirmed(snap) {
		f := snap.Findings[0]
		for _, id := range f.EvidenceIDs {
			if _, err := e.store.GetEvidence(context.Background(), id); err != nil {
				t.Errorf("finding references missing evidence %s: %v", id, err)
			}
		}
		t.Logf("confirmed: %s", f.Title)
	} else {
		t.Logf("not confirmed within budget (stop_reason=%s) — a real small model may not; this is logged, not failed", snap.Run.StopReason)
	}
	if snap.Run.Status == model.RunFailed {
		t.Fatalf("infrastructure failure: %+v", snap.Run)
	}
	if len(snap.Decisions) == 0 {
		t.Fatal("model never reached a recorded decision")
	}
}

// TestLiveAgentDoesNotConfirmInFixedMode is the real-model counterpart of
// AC-02: a false confirmed here would be a genuine Verifier bug, not model
// variance, since the rule relies only on executed evidence.
func TestLiveAgentDoesNotConfirmInFixedMode(t *testing.T) {
	client, modelID := liveClient(t)
	e := newEnv(t, opts{mode: labapp.ModeFixed})

	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-live-2", Goal: "check cross-account document access",
		ModelID: modelID, ModelOptions: client.Options(),
		Planner: &planner.OllamaPlanner{Client: client}, Judge: judge.OllamaJudge{Client: client},
		Exec: e.exec, Store: e.store, Policy: e.pol, Budget: e.budget,
	})
	snap, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s: stop_reason=%s status=%s", modelID, snap.Run.StopReason, snap.Run.Status)
	if confirmed(snap) {
		t.Fatalf("false confirmed in fixed mode: %+v", snap.Verifications)
	}
}
