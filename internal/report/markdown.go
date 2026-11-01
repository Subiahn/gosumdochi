package report

import (
	"fmt"
	"strings"
)

// Markdown renders the human-readable counterpart of report.json. Every
// claim in it traces back to an id present in the Report.
func Markdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# gosumdochi report: %s\n\n", r.Run.ID)
	fmt.Fprintf(&b, "Generated %s · schema v%d\n\n", r.GeneratedAt.Format("2006-01-02T15:04:05Z"), r.SchemaVersion)

	fmt.Fprintln(&b, "## Run")
	fmt.Fprintln(&b, "| field | value |")
	fmt.Fprintln(&b, "| --- | --- |")
	rows := [][2]string{
		{"goal", r.Run.Goal}, {"model_id", r.Run.ModelID}, {"status", string(r.Run.Status)},
		{"stop_reason", string(r.Run.StopReason)}, {"cycles", fmt.Sprint(r.Run.Cycles)},
		{"model_calls", fmt.Sprint(r.Run.ModelCalls)}, {"http_requests", fmt.Sprint(r.Run.HTTPRequests)},
		{"config_digest", r.Run.ConfigDigest},
	}
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s |\n", row[0], escapeCell(row[1]))
	}
	b.WriteString("\n")

	if len(r.Findings) == 0 {
		fmt.Fprint(&b, "## Findings\n\nNone confirmed in this run. This does not mean the target is safe; see Limitations.\n\n")
	} else {
		fmt.Fprintln(&b, "## Findings")
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "\n### [%s] %s — %s\n\n%s\n\n- verification: `%s`\n- evidence: %s\n",
				strings.ToUpper(string(f.Severity)), f.ID, f.Title, f.Impact, f.VerificationID, inlineList(f.EvidenceIDs))
			if len(f.Limitations) > 0 {
				fmt.Fprintf(&b, "- limitations: %s\n", strings.Join(f.Limitations, "; "))
			}
		}
		b.WriteString("\n")
	}

	fmt.Fprintln(&b, "## Rule summary")
	fmt.Fprintln(&b, "| rule_id | confirmed | rejected | inconclusive |")
	fmt.Fprintln(&b, "| --- | --- | --- | --- |")
	for _, c := range r.RuleSummary {
		fmt.Fprintf(&b, "| %s | %d | %d | %d |\n", c.RuleID, c.Confirmed, c.Rejected, c.Inconclusive)
	}
	b.WriteString("\n")

	fmt.Fprintln(&b, "## Verifications")
	fmt.Fprintln(&b, "| id | hypothesis | outcome | rule_id | reasons |")
	fmt.Fprintln(&b, "| --- | --- | --- | --- | --- |")
	for _, v := range r.Verifications {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", v.ID, v.HypothesisID, v.Outcome, v.RuleID, escapeCell(strings.Join(v.Reasons, "; ")))
	}
	b.WriteString("\n")

	fmt.Fprintln(&b, "## Hypotheses")
	fmt.Fprintln(&b, "| id | status | claim |")
	fmt.Fprintln(&b, "| --- | --- | --- |")
	for _, h := range r.Hypotheses {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", h.ID, h.Status, escapeCell(h.Claim))
	}
	b.WriteString("\n")

	fmt.Fprintln(&b, "## Limitations")
	for _, l := range r.Limitations {
		fmt.Fprintf(&b, "- %s\n", l)
	}
	return b.String()
}

func inlineList(ids []string) string {
	if len(ids) == 0 {
		return "(none)"
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = "`" + id + "`"
	}
	return strings.Join(out, ", ")
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}
