// Package verifier decides a hypothesis from collected evidence only. It never
// sees the evaluator's answer key, the app mode, or expected IDs/markers:
// owners and markers are compared against what account B's own baseline
// response showed. The rule is specific to this practice scenario and is not a
// general authorization checker.
package verifier

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"gosumdochi/internal/model"
)

const RuleID = "doc-cross-read-v1"

var markerRE = regexp.MustCompile(`LAB-MARK-[A-Za-z0-9]+`)

// Record is one piece of evidence together with what the agent knows about
// the request that produced it.
type Record struct {
	EvidenceID string
	Account    model.AccountRef
	URL        string
	Path       string // URL path
	Status     int
	Usable     bool // complete body that parsed as JSON
	UserID     string
	OwnerID    string
	Marker     string
}

// NewRecord extracts the facts a rule may use. A truncated or unparsable body
// yields an unusable record that can never support a conclusion.
func NewRecord(ev model.Evidence, url, path string) Record {
	r := Record{EvidenceID: ev.ID, Account: ev.AccountRef, URL: url, Path: path}
	head, body, _ := strings.Cut(ev.SanitizedResponse, "\n\n")
	fmt.Sscanf(head, "HTTP %d", &r.Status)
	if ev.Truncated {
		return r
	}
	var v struct {
		UserID  string `json:"user_id"`
		OwnerID string `json:"owner_id"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return r
	}
	r.Usable = true
	r.UserID, r.OwnerID = v.UserID, v.OwnerID
	r.Marker = markerRE.FindString(v.Body)
	return r
}

// Verify evaluates the cross-read hypothesis over all records.
// identityPath is the configured identity endpoint.
func Verify(records []Record, identityPath string) model.Verification {
	v := model.Verification{RuleID: RuleID, Outcome: model.OutcomeInconclusive}

	idA, okA := identity(records, model.AccountA, identityPath)
	idB, okB := identity(records, model.AccountB, identityPath)
	if !okA || !okB {
		v.Reasons = append(v.Reasons, "identity check missing or failed for "+missingAccounts(okA, okB))
		return v
	}
	ids := map[model.AccountRef]Record{model.AccountA: idA, model.AccountB: idB}
	if idA.UserID == idB.UserID {
		v.Reasons = append(v.Reasons, "accounts A and B resolve to the same identity")
		return v
	}

	// Try both directions: reader X, owner Y.
	var denied *model.Verification
	var notes []string
	for _, dir := range [][2]model.AccountRef{{model.AccountA, model.AccountB}, {model.AccountB, model.AccountA}} {
		reader, owner := dir[0], dir[1]
		res, ok := checkDirection(records, ids, reader, owner)
		switch res.Outcome {
		case model.OutcomeConfirmed:
			res.RuleID = RuleID
			return res
		case model.OutcomeRejected:
			if ok && denied == nil {
				res.RuleID = RuleID
				denied = &res
			}
		}
		notes = append(notes, res.Reasons...)
	}
	if denied != nil {
		return *denied
	}
	v.Reasons = dedupe(notes)
	return v
}

func identity(rs []Record, acct model.AccountRef, identityPath string) (Record, bool) {
	for _, r := range rs {
		if r.Account == acct && r.Path == identityPath && r.Status == 200 && r.Usable && r.UserID != "" {
			return r, true
		}
	}
	return Record{}, false
}

func missingAccounts(okA, okB bool) string {
	switch {
	case !okA && !okB:
		return "A and B"
	case !okA:
		return "A"
	}
	return "B"
}

// checkDirection tests whether reader can read a document owned by owner.
// ok is true when the result rests on a complete baseline (needed to reject).
func checkDirection(rs []Record, ids map[model.AccountRef]Record, reader, owner model.AccountRef) (model.Verification, bool) {
	tag := fmt.Sprintf("%s reads %s's document: ", reader, owner)
	v := model.Verification{Outcome: model.OutcomeInconclusive}
	ownerID, readerID := ids[owner].UserID, ids[reader].UserID

	// Documents the owner read as themselves: baseline candidates.
	var baselines []Record
	for _, r := range rs {
		if r.Account == owner && r.Status == 200 && r.Usable && r.OwnerID == ownerID && r.Marker != "" {
			baselines = append(baselines, r)
		}
	}
	if len(baselines) == 0 {
		v.Reasons = append(v.Reasons, tag+"no baseline: owner's own document response with owner and marker")
		return v, false
	}

	sawCross := false
	for _, base := range baselines {
		var cross []Record
		var blocked []Record
		for _, r := range rs {
			if r.Account != reader || r.URL != base.URL {
				continue
			}
			sawCross = true
			switch {
			case r.Status == 200 && r.Usable && r.OwnerID == ownerID && r.Marker == base.Marker:
				cross = append(cross, r)
			case r.Status == 403 || r.Status == 404:
				blocked = append(blocked, r)
			}
		}
		if len(cross) > 0 {
			ctrl, hasCtrl := control(rs, reader, readerID, base.Marker)
			switch {
			case !hasCtrl:
				v.Reasons = append(v.Reasons, tag+"no control: reader's own document response")
			case len(cross) < 2:
				v.Reasons = append(v.Reasons, tag+"not reproduced: need an independent second read")
			default:
				v.Outcome = model.OutcomeConfirmed
				v.BaselineIDs = []string{ids[reader].EvidenceID, ids[owner].EvidenceID, base.EvidenceID, ctrl.EvidenceID}
				for _, c := range cross {
					v.ComparisonIDs = append(v.ComparisonIDs, c.EvidenceID)
				}
				v.Reasons = []string{
					fmt.Sprintf("%s read a document whose owner field and marker match %s's own baseline response", reader, owner),
					"reader's own-document control differs in marker", "reproduced by an independent second read",
				}
				return v, true
			}
			continue
		}
		if len(blocked) > 0 {
			v.Outcome = model.OutcomeRejected
			v.BaselineIDs = []string{ids[reader].EvidenceID, ids[owner].EvidenceID, base.EvidenceID}
			for _, b := range blocked {
				v.ComparisonIDs = append(v.ComparisonIDs, b.EvidenceID)
			}
			v.Reasons = []string{tag + "access was refused (403/404) for the observed document; this says nothing about other resources"}
			return v, true
		}
	}
	if !sawCross {
		v.Reasons = append(v.Reasons, tag+"no cross-account read attempted yet")
	} else {
		v.Reasons = append(v.Reasons, tag+"responses lack matching owner/marker evidence (status alone is not enough)")
	}
	return v, true
}

// control finds the reader's own document response, which must carry a
// different marker than the owner's, ruling out session mix-ups.
func control(rs []Record, reader model.AccountRef, readerID, ownerMarker string) (Record, bool) {
	for _, r := range rs {
		if r.Account == reader && r.Status == 200 && r.Usable && r.OwnerID == readerID && r.Marker != "" && r.Marker != ownerMarker {
			return r, true
		}
	}
	return Record{}, false
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
