package verifier

import (
	"encoding/json"
	"fmt"
	"strings"

	"gosumdochi/internal/model"
)

const VerticalPrivescRuleID = "vertical-privesc-v1"

const adminUsersPath = "/api/admin/users"

// AdminRecord is a read of the admin-only user list, independent of Record
// (the response shape has no owner/marker fields to compare).
type AdminRecord struct {
	EvidenceID string
	Account    model.AccountRef
	Path       string
	Status     int
	// HasUserList is true when the body parsed as JSON with a non-empty
	// "users" array -- the shape only the admin endpoint returns.
	HasUserList bool
}

// NewAdminRecord extracts the facts VerifyVerticalPrivesc needs. A truncated
// body is never treated as a user list.
func NewAdminRecord(ev model.Evidence, path string) AdminRecord {
	r := AdminRecord{EvidenceID: ev.ID, Account: ev.AccountRef, Path: path}
	head, body, _ := strings.Cut(ev.SanitizedResponse, "\n\n")
	fmt.Sscanf(head, "HTTP %d", &r.Status)
	if ev.Truncated {
		return r
	}
	var v struct {
		Users []json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal([]byte(body), &v); err == nil && len(v.Users) > 0 {
		r.HasUserList = true
	}
	return r
}

// VerifyVerticalPrivesc reuses doc-cross-read-v1's baseline/comparison shape:
// baseline is the admin account's own successful read of the admin endpoint,
// comparison is the regular account's read of the same endpoint. There is no
// per-account marker here (the data isn't account-specific), so confirmation
// rests on the regular account receiving the same user-list shape the admin
// account gets, not on content equality.
func VerifyVerticalPrivesc(records []AdminRecord, adminAccount, regularAccount model.AccountRef) model.Verification {
	v := model.Verification{RuleID: VerticalPrivescRuleID, Outcome: model.OutcomeInconclusive}

	var baseline, comparison *AdminRecord
	for i := range records {
		r := &records[i]
		if r.Path != adminUsersPath {
			continue
		}
		switch r.Account {
		case adminAccount:
			if baseline == nil && r.Status == 200 && r.HasUserList {
				baseline = r
			}
		case regularAccount:
			if comparison == nil || (comparison.Status != 200 && comparison.Status != 403) {
				comparison = r // prefer the first definitive (200 or 403) result
			}
		}
	}
	if baseline == nil {
		v.Reasons = []string{"no baseline: admin account's own successful read of " + adminUsersPath}
		return v
	}
	if comparison == nil {
		v.Reasons = []string{"regular account has not attempted " + adminUsersPath + " yet"}
		return v
	}
	switch {
	case comparison.Status == 200 && comparison.HasUserList:
		v.Outcome = model.OutcomeConfirmed
		v.BaselineIDs = []string{baseline.EvidenceID}
		v.ComparisonIDs = []string{comparison.EvidenceID}
		v.Reasons = []string{"the regular account received the same admin user list the admin account gets"}
	case comparison.Status == 403 || comparison.Status == 401:
		v.Outcome = model.OutcomeRejected
		v.BaselineIDs = []string{baseline.EvidenceID}
		v.ComparisonIDs = []string{comparison.EvidenceID}
		v.Reasons = []string{fmt.Sprintf("regular account was refused %s (status %d)", adminUsersPath, comparison.Status)}
	default:
		v.Reasons = []string{fmt.Sprintf("regular account's response (status %d) neither confirms nor rejects", comparison.Status)}
	}
	return v
}
