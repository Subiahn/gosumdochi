package model

import (
	"errors"
	"fmt"
)

// ErrorCode classifies validation failures. Codes are stable across a schema version.
type ErrorCode string

const (
	CodeSchemaVersion  ErrorCode = "schema_version"
	CodeMissingField   ErrorCode = "missing_field"
	CodeUnknownEnum    ErrorCode = "unknown_enum"
	CodeUnknownRef     ErrorCode = "unknown_reference"
	CodeUntrustedRef   ErrorCode = "untrusted_reference"
	CodeTooMany        ErrorCode = "too_many"
	CodeInvalidValue   ErrorCode = "invalid_value"
	CodeDecode         ErrorCode = "decode"
	CodeInvalidFinding ErrorCode = "invalid_finding"
)

type ValidationError struct {
	Code  ErrorCode
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Field, e.Msg)
}

// CodeOf returns the ErrorCode of err, or "" if err is not a ValidationError.
func CodeOf(err error) ErrorCode {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Code
	}
	return ""
}

func missing(field string) error {
	return &ValidationError{Code: CodeMissingField, Field: field, Msg: "required"}
}

func invalid(field, msg string) error {
	return &ValidationError{Code: CodeInvalidValue, Field: field, Msg: msg}
}
