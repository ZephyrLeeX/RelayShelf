package jobs

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrStateTransition = errors.New("job state transition affected no rows")
var ErrJobClaimLost = errors.New("job claim lost")

type JobClaimLostError struct {
	JobID        uuid.UUID
	DesiredState string
	CurrentState string
}

func (e *JobClaimLostError) Error() string {
	return fmt.Sprintf("job %s claim lost: cannot transition from %s to %s", e.JobID, e.CurrentState, e.DesiredState)
}

func (e *JobClaimLostError) Unwrap() error { return ErrJobClaimLost }

type HandlerError struct {
	Code      string
	Summary   string
	Permanent bool
}

func (e *HandlerError) Error() string { return e.Code }

func Retryable(code, summary string) error {
	return &HandlerError{Code: safeCode(code), Summary: safeSummary(summary)}
}

func Permanent(code, summary string) error {
	return &HandlerError{Code: safeCode(code), Summary: safeSummary(summary), Permanent: true}
}

func classifyError(err error) (code, summary string, permanent bool) {
	var target *HandlerError
	if errors.As(err, &target) {
		return safeCode(target.Code), safeSummary(target.Summary), target.Permanent
	}
	return "JOB_HANDLER_FAILED", "job handler failed", false
}

func safeCode(value string) string {
	if value == "" || len(value) > 64 {
		return "JOB_HANDLER_FAILED"
	}
	for _, r := range value {
		if r != '_' && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return "JOB_HANDLER_FAILED"
		}
	}
	return value
}

func safeSummary(value string) string {
	if !utf8.ValidString(value) || value == "" {
		return "job handler failed"
	}
	for len(value) > 512 {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
