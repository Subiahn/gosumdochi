// Package executor runs one approved action end to end: policy check, tool
// call, masking, evidence storage and observation building.
package executor

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"gosumdochi/internal/evidence"
	"gosumdochi/internal/extract"
	"gosumdochi/internal/model"
	"gosumdochi/internal/policy"
	"gosumdochi/internal/state"
	"gosumdochi/internal/tools"
)

type Executor struct {
	RunID   string
	Policy  *policy.Policy
	Tool    tools.Tool
	Store   state.Store
	Masker  *evidence.Masker
	Summary int // observation summary cap in bytes; 0 = extract.DefaultMaxSummary
	Now     func() time.Time

	mu     sync.Mutex
	evSeq  int
	obsSeq int
}

func (e *Executor) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Executor) nextIDs(needEvidence bool) (obs, ev string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.obsSeq++
	obs = fmt.Sprintf("O-%04d", e.obsSeq)
	if needEvidence {
		e.evSeq++
		ev = fmt.Sprintf("E-%04d", e.evSeq)
	}
	return
}

// Run executes a. Policy denials, tool failures and cancellation yield an
// Observation with the matching ResultStatus; an error is returned only when
// the record itself cannot be stored.
func (e *Executor) Run(ctx context.Context, a model.Action) (model.Observation, error) {
	if err := e.Policy.CheckAction(a); err != nil {
		return e.record(ctx, a, model.ResultDenied, "denied: "+policy.DeniedCode(err), nil)
	}
	res, err := e.Tool.Execute(ctx, a)
	if err != nil {
		return e.record(ctx, a, model.ResultFailed, "tool error", nil)
	}
	if res.Status != model.ResultCompleted {
		return e.record(ctx, a, res.Status, string(res.Status)+": "+res.ErrorCode, nil)
	}
	return e.record(ctx, a, model.ResultCompleted, "", &res)
}

func (e *Executor) record(ctx context.Context, a model.Action, st model.ResultStatus, note string, res *tools.Result) (model.Observation, error) {
	obsID, evID := e.nextIDs(res != nil)
	meta := model.Meta{SchemaVersion: model.SchemaVersion, RunID: e.RunID, Timestamp: e.now().UTC()}
	obs := model.Observation{Meta: meta, ID: obsID, ActionID: a.ID, AccountRef: a.TypedArgs.AccountRef, URL: a.TypedArgs.URL, ResultStatus: st, EvidenceIDs: []string{}, Summary: note}
	if res != nil {
		ev := evidence.Build(e.Masker, evidence.Input{
			ID: evID, ActionID: a.ID, Account: a.TypedArgs.AccountRef,
			Request: requestLine(a), Status: res.HTTPStatus, Headers: res.Headers, Body: res.Body,
			Truncated: res.Truncated, ContentLength: res.ContentLength, RunID: e.RunID, Now: e.now(),
		})
		if err := e.Store.PutEvidence(ctx, ev); err != nil {
			return obs, err
		}
		obs.EvidenceIDs = []string{evID}
		e.summarize(&obs, a, res)
	}
	if err := e.Store.AppendEvent(ctx, "observation", obs); err != nil {
		return obs, err
	}
	return obs, nil
}

// summarize builds the model-facing summary from the masked body only.
func (e *Executor) summarize(obs *model.Observation, a model.Action, res *tools.Result) {
	obs.Summary = fmt.Sprintf("HTTP %d", res.HTTPStatus)
	limit := e.Summary
	if limit <= 0 {
		limit = extract.DefaultMaxSummary
	}
	limit -= len(obs.Summary) + 1 // keep the whole summary within the cap
	base, err := url.Parse(a.TypedArgs.URL)
	ct := ""
	if v := res.Headers["Content-Type"]; len(v) > 0 {
		ct = v[0]
	}
	var r extract.Result
	if err == nil {
		r, err = extract.Extract(ct, []byte(e.Masker.Mask(res.Body)), base, limit)
	}
	if err != nil || res.Truncated {
		// A cut body may parse as a plausible but partial summary: treat as failed.
		obs.ExtractionFailed = true
		obs.Summary += " (summary unavailable)"
		return
	}
	obs.Summary += "\n" + r.Summary
	obs.ExposedRefs = r.Refs
	obs.DiscoveredParams = r.Params
}

func requestLine(a model.Action) string {
	s := fmt.Sprintf("%s %s (account %s)", a.TypedArgs.Method, a.TypedArgs.URL, a.TypedArgs.AccountRef)
	if a.TypedArgs.MarkerID != "" {
		s += fmt.Sprintf(" [param=%s marker=%s]", a.TypedArgs.ParamName, a.TypedArgs.MarkerID)
	}
	return s
}
