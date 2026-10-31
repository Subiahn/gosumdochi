// Command gosumdochi is the CLI: plan validates configuration with no
// network, run executes the agent, report regenerates report.json/report.md
// from a stored run with no network. See docs/SDD.md §11.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gosumdochi/internal/agent"
	"gosumdochi/internal/config"
	"gosumdochi/internal/evidence"
	"gosumdochi/internal/executor"
	"gosumdochi/internal/judge"
	"gosumdochi/internal/llm/ollama"
	"gosumdochi/internal/model"
	"gosumdochi/internal/planner"
	"gosumdochi/internal/policy"
	"gosumdochi/internal/report"
	"gosumdochi/internal/session"
	"gosumdochi/internal/state"
	httptool "gosumdochi/internal/tools/http"
)

// Exit codes (SDD §11). 0 is "ran to its intended stop", never "no finding".
const (
	exitOK            = 0
	exitConfigOrInfra = 1
	exitIncomplete    = 2
	exitCanceled      = 130
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(exitConfigOrInfra)
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = cmdPlan(os.Args[2:])
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	case "report":
		err = cmdReport(os.Args[2:])
	default:
		usage()
		os.Exit(exitConfigOrInfra)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "gosumdochi:", err)
		os.Exit(exitConfigOrInfra)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  gosumdochi plan   -config <path>
  gosumdochi run    -config <path> [-goal <text>] [-run-id <id>]
  gosumdochi report -run <dir>`)
}

func loadConfig(path string) (config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return config.Config{}, err
	}
	return config.Parse(data)
}

// cmdPlan validates configuration only. It opens no socket: config.Parse
// parses and checks target_origin/CIDRs/paths as strings, never resolves or
// connects to them.
func cmdPlan(args []string) error {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	path := fs.String("config", "", "config file path")
	fs.Parse(args)
	if *path == "" {
		return fmt.Errorf("plan: -config is required")
	}
	cfg, err := loadConfig(*path)
	if err != nil {
		return fmt.Errorf("plan: %w", err)
	}
	fmt.Printf("config OK: target=%s accounts=[A,B] llm=%s/%s budgets={cycles:%d http:%d model_calls:%d} digest=%s\n",
		cfg.TargetOrigin, cfg.LLM.Provider, cfg.LLM.Model, cfg.Budgets.MaxCycles, cfg.Budgets.MaxHTTPRequests, cfg.Budgets.MaxModelCalls, cfg.Digest()[:16])
	return nil
}

func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	dir := fs.String("run", "", "run directory (contains state.json)")
	fs.Parse(args)
	if *dir == "" {
		return fmt.Errorf("report: -run is required")
	}
	snap, err := state.ReadSnapshot(*dir)
	if err != nil {
		return fmt.Errorf("report: read state: %w", err)
	}
	r := report.Build(snap, time.Now())
	if err := report.WriteFiles(*dir, r); err != nil {
		return fmt.Errorf("report: write: %w", err)
	}
	fmt.Printf("report written: %s (status=%s findings=%d)\n", filepath.Join(*dir, "report.json"), snap.Run.Status, len(snap.Findings))
	return nil
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	path := fs.String("config", "", "config file path")
	goal := fs.String("goal", "check cross-account document access", "run goal, recorded in run metadata")
	runID := fs.String("run-id", "", "run id; default: time-based, random suffix")
	fs.Parse(args)
	if *path == "" {
		fmt.Fprintln(os.Stderr, "run: -config is required")
		return exitConfigOrInfra
	}
	cfg, err := loadConfig(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: config:", err)
		return exitConfigOrInfra
	}
	if *runID == "" {
		*runID = newRunID()
	}
	runDir := filepath.Join(cfg.OutputDir, *runID)
	// A run-id is used exactly once: reusing one (e.g. a stale -run-id, or a
	// retried script) must fail loudly here, not corrupt the earlier run's
	// events.jsonl or collide on an existing evidence file deep inside a cycle.
	if _, err := os.Stat(filepath.Join(runDir, "state.json")); err == nil {
		fmt.Fprintf(os.Stderr, "run: %s already has a stored run; pick a different -run-id\n", runDir)
		return exitConfigOrInfra
	}
	if _, err := os.Stat(filepath.Join(runDir, "events.jsonl")); err == nil {
		fmt.Fprintf(os.Stderr, "run: %s already has an event log; pick a different -run-id\n", runDir)
		return exitConfigOrInfra
	}
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
		return exitConfigOrInfra
	}

	sm, err := session.NewManager(cfg, session.ResolveRef)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: accounts:", err)
		return exitConfigOrInfra
	}
	pol, err := policy.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: policy:", err)
		return exitConfigOrInfra
	}
	budget := policy.NewBudget(cfg.Budgets.MaxHTTPRequests)
	masker := evidence.NewMasker(sm.Secrets()...)
	tool, err := httptool.New(cfg, pol, budget, sm, masker, httptool.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: http tool:", err)
		return exitConfigOrInfra
	}
	store, err := state.NewFileStore(runDir, *runID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: store:", err)
		return exitConfigOrInfra
	}
	defer store.Close()
	exec := &executor.Executor{RunID: *runID, Policy: pol, Tool: tool, Store: store, Masker: masker}

	llmTimeout := time.Duration(cfg.Budgets.LLMTimeoutSec) * time.Second
	client, err := ollama.New(cfg.LLM.BaseURL, cfg.LLM.Model, llmTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: llm client:", err)
		return exitConfigOrInfra
	}
	resolveCtx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	modelID, err := client.ResolveModel(resolveCtx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "run: resolve model:", err)
		return exitConfigOrInfra
	}

	a := agent.New(agent.Deps{
		Config: cfg, RunID: *runID, Goal: *goal, ModelID: modelID, ModelOptions: client.Options(),
		Planner: &planner.OllamaPlanner{Client: client}, Judge: judge.OllamaJudge{Client: client},
		Exec: exec, Store: store, Policy: pol, Budget: budget,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "gosumdochi: run %s starting (model %s)\n", *runID, modelID)
	snap, runErr := a.Run(ctx)
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "run: agent:", runErr)
		return exitConfigOrInfra
	}

	rep := report.Build(snap, time.Now())
	if err := report.WriteFiles(runDir, rep); err != nil {
		fmt.Fprintln(os.Stderr, "run: report:", err)
		return exitConfigOrInfra
	}
	fmt.Fprintf(os.Stderr, "gosumdochi: run %s done: status=%s stop_reason=%s findings=%d report=%s\n",
		*runID, snap.Run.Status, snap.Run.StopReason, len(snap.Findings), filepath.Join(runDir, "report.json"))

	return exitCodeFor(snap.Run)
}

func exitCodeFor(run model.Run) int {
	switch run.Status {
	case model.RunCompleted:
		return exitOK
	case model.RunCanceled:
		return exitCanceled
	case model.RunFailed:
		return exitConfigOrInfra
	default: // incomplete
		return exitIncomplete
	}
}

func newRunID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "run-" + time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}
