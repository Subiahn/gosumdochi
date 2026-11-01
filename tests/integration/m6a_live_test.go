// M6a live check: the real Ollama adapter drives Planner and Judge against
// the three new rules (SDD §14.1), with no fake. Skipped unless
// OLLAMA_LIVE_MODEL is set (see m4_test.go's liveClient).
package integration

import (
	"context"
	"testing"

	"gosumdochi/internal/agent"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/labapp"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
)

// TestLiveM6aVulnerableMode runs the real model once in vulnerable mode and
// reports every M6a hypothesis's outcome. passive-exposure-v1 is asserted
// hard (it needs no model skill: the debug header is on every response).
// vertical-privesc-v1 and unauth-access-v1 are logged, not required: whether
// a small local model discovers them on its own is the open question this
// test measures, not a correctness property of the code.
func TestLiveM6aVulnerableMode(t *testing.T) {
	client, modelID := liveClient(t)
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})

	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-m6a-vuln", Goal: "check cross-account document access and related access-control issues",
		ModelID: modelID, ModelOptions: client.Options(),
		Planner: &planner.OllamaPlanner{Client: client}, Judge: judge.OllamaJudge{Client: client},
		Exec: e.exec, Store: e.store, Policy: e.pol, Budget: e.budget,
	})
	snap, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s: stop_reason=%s status=%s cycles=%d decisions=%d http_requests=%d",
		modelID, snap.Run.StopReason, snap.Run.Status, snap.Run.Cycles, len(snap.Decisions), e.budget.Used())
	for _, id := range []string{agent.PrimaryHypothesis, agent.HypPrivesc, agent.HypUnauth, agent.HypPassive} {
		h := hypothesis(snap, id)
		t.Logf("%s: status=%s claim=%q", id, h.Status, h.Claim)
	}
	if snap.Run.Status == model.RunFailed {
		t.Fatalf("infrastructure failure: %+v", snap.Run)
	}
	if hypothesis(snap, agent.HypPassive).Status != model.HypConfirmed {
		t.Fatalf("passive-exposure-v1 needs no model skill and should always confirm here: %+v", hypothesis(snap, agent.HypPassive))
	}
	if _, ok := finding(snap, agent.HypPassive); !ok {
		t.Fatal("passive-exposure-v1 confirmed but produced no finding")
	}
}

// TestLiveM6aFixedMode asserts the negative side strictly: none of the three
// rules may falsely confirm in fixed mode, regardless of what the model
// tries. This is the property that actually matters for safety.
func TestLiveM6aFixedMode(t *testing.T) {
	client, modelID := liveClient(t)
	e := newEnv(t, opts{mode: labapp.ModeFixed})

	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-m6a-fixed", Goal: "check cross-account document access and related access-control issues",
		ModelID: modelID, ModelOptions: client.Options(),
		Planner: &planner.OllamaPlanner{Client: client}, Judge: judge.OllamaJudge{Client: client},
		Exec: e.exec, Store: e.store, Policy: e.pol, Budget: e.budget,
	})
	snap, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s: stop_reason=%s status=%s", modelID, snap.Run.StopReason, snap.Run.Status)
	for _, id := range []string{agent.PrimaryHypothesis, agent.HypPrivesc, agent.HypUnauth, agent.HypPassive} {
		h := hypothesis(snap, id)
		t.Logf("%s: status=%s", id, h.Status)
		if h.Status == model.HypConfirmed {
			t.Errorf("%s falsely confirmed in fixed mode", id)
		}
	}
	if hypothesis(snap, agent.HypPassive).Status != model.HypRejected {
		t.Errorf("passive-exposure-v1 should reject (no debug header in fixed mode): %+v", hypothesis(snap, agent.HypPassive))
	}
}
