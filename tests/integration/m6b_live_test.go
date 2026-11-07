// M6b live check: the real Ollama adapter drives Planner and Judge against
// reflected-input-v1 (ADR-0002), with no fake. Skipped unless
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

// TestLiveM6bVulnerableMode runs the real model once in vulnerable mode.
// Unlike passive-exposure-v1 (M6a), reflected-input-v1 needs real model
// skill: the model must notice "discovered_params" on an observation and
// choose a param_name + marker_id for H-REFLECTED. So this is logged, not
// hard-required -- whether a small local model finds it on its own within
// budget is the open question this test measures.
func TestLiveM6bVulnerableMode(t *testing.T) {
	client, modelID := liveClient(t)
	e := newEnv(t, opts{mode: labapp.ModeVulnerable})

	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-m6b-vuln", Goal: "check cross-account document access and related access-control issues, including reflected-input (XSS/SQLi) on any discovered parameter",
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
	for _, id := range []string{agent.PrimaryHypothesis, agent.HypPrivesc, agent.HypUnauth, agent.HypPassive, agent.HypReflected} {
		h := hypothesis(snap, id)
		t.Logf("%s: status=%s claim=%q", id, h.Status, h.Claim)
	}
	reflectedTried := false
	for _, act := range snap.Actions {
		if act.TypedArgs.MarkerID != "" {
			reflectedTried = true
			t.Logf("reflected-input probe: param=%s marker=%s account=%s", act.TypedArgs.ParamName, act.TypedArgs.MarkerID, act.TypedArgs.AccountRef)
		}
	}
	t.Logf("reflected-input-v1 ever attempted by the model: %v", reflectedTried)
	if snap.Run.Status == model.RunFailed {
		t.Fatalf("infrastructure failure: %+v", snap.Run)
	}
}

// TestLiveM6bFixedMode asserts the negative side strictly: reflected-input-v1
// may not falsely confirm in fixed mode, regardless of what the model tries
// (the search endpoint HTML-escapes its echo there).
func TestLiveM6bFixedMode(t *testing.T) {
	client, modelID := liveClient(t)
	e := newEnv(t, opts{mode: labapp.ModeFixed})

	a := agent.New(agent.Deps{
		Config: e.cfg, RunID: "run-m6b-fixed", Goal: "check cross-account document access and related access-control issues, including reflected-input (XSS/SQLi) on any discovered parameter",
		ModelID: modelID, ModelOptions: client.Options(),
		Planner: &planner.OllamaPlanner{Client: client}, Judge: judge.OllamaJudge{Client: client},
		Exec: e.exec, Store: e.store, Policy: e.pol, Budget: e.budget,
	})
	snap, err := a.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("model %s: stop_reason=%s status=%s", modelID, snap.Run.StopReason, snap.Run.Status)
	h := hypothesis(snap, agent.HypReflected)
	t.Logf("%s: status=%s", agent.HypReflected, h.Status)
	if h.Status == model.HypConfirmed {
		t.Errorf("reflected-input-v1 falsely confirmed in fixed mode")
	}
}
