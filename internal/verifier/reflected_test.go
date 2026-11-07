package verifier

import (
	"testing"

	"gosumdochi/internal/model"
)

func TestVerifyReflectedInputConfirmedOnXSS(t *testing.T) {
	records := []ReflectedRecord{
		NewReflectedRecord(model.Evidence{ID: "E-1", SanitizedResponse: "HTTP 200\n\n{\"query\":\"gsdq-marker-7f3a21\"}"}, model.MarkerBenign, "gsdq-marker-7f3a21"),
		NewReflectedRecord(model.Evidence{ID: "E-2", SanitizedResponse: "HTTP 200\n\n{\"query\":\"<script>alert(1)</script>\"}"}, model.MarkerXSSScript, "<script>alert(1)</script>"),
	}
	v := VerifyReflectedInput(records)
	if v.Outcome != model.OutcomeConfirmed || len(v.ComparisonIDs) != 1 || v.ComparisonIDs[0] != "E-2" {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyReflectedInputConfirmedOnSQLiErrorPattern(t *testing.T) {
	records := []ReflectedRecord{
		NewReflectedRecord(model.Evidence{ID: "E-1", SanitizedResponse: "HTTP 500\n\nSQLite error: unrecognized token: \"'\""}, model.MarkerSQLiQuote, "'"),
	}
	v := VerifyReflectedInput(records)
	if v.Outcome != model.OutcomeConfirmed || v.ComparisonIDs[0] != "E-1" {
		t.Fatalf("%+v", v)
	}
}

// The benign marker has no metacharacters: finding it verbatim proves
// nothing about escaping, so it must never confirm by itself.
func TestVerifyReflectedInputBenignMarkerNeverConfirms(t *testing.T) {
	records := []ReflectedRecord{
		NewReflectedRecord(model.Evidence{ID: "E-1", SanitizedResponse: "HTTP 200\n\n{\"query\":\"gsdq-marker-7f3a21\"}"}, model.MarkerBenign, "gsdq-marker-7f3a21"),
	}
	v := VerifyReflectedInput(records)
	if v.Outcome != model.OutcomeRejected {
		t.Fatalf("%+v", v)
	}
}

// Escaped reflection (fixed mode: html.EscapeString turns "<" into "&lt;")
// must not confirm: the literal payload no longer appears verbatim.
func TestVerifyReflectedInputRejectedWhenEscaped(t *testing.T) {
	records := []ReflectedRecord{
		NewReflectedRecord(model.Evidence{ID: "E-1", SanitizedResponse: "HTTP 200\n\n{\"query\":\"\\u0026lt;script\\u0026gt;alert(1)\\u0026lt;/script\\u0026gt;\"}"}, model.MarkerXSSScript, "<script>alert(1)</script>"),
	}
	v := VerifyReflectedInput(records)
	if v.Outcome != model.OutcomeRejected {
		t.Fatalf("%+v", v)
	}
}

func TestVerifyReflectedInputEmptyRecords(t *testing.T) {
	v := VerifyReflectedInput(nil)
	if v.Outcome != model.OutcomeInconclusive {
		t.Fatalf("%+v", v)
	}
}
