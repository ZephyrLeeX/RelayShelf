package jobs

import (
	"strings"
	"testing"
	"time"
)

func TestFiniteExponentialBackoff(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, v := range want {
		if got := Backoff(i + 1); got != v {
			t.Fatalf("attempt %d delay=%v want=%v", i+1, got, v)
		}
	}
}
func TestSafeJobError(t *testing.T) {
	code, summary, permanent := classifyError(Permanent("THUMBNAIL_DECODE_FAILED", strings.Repeat("x", 600)))
	if code != "THUMBNAIL_DECODE_FAILED" || !permanent || len(summary) > 512 {
		t.Fatalf("code=%q permanent=%v summary bytes=%d", code, permanent, len(summary))
	}
	code, summary, _ = classifyError(assertError("/secret/path"))
	if code != "JOB_HANDLER_FAILED" || summary != "job handler failed" {
		t.Fatalf("unsafe fallback %q %q", code, summary)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }
