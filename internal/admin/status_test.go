package admin

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStorageProbeTimeoutLeavesAClosedGateForFastDegradation(t *testing.T) {
	gate := make(chan struct{}, 1)
	unblock := make(chan struct{})
	start := time.Now()
	_, err := boundedProbe(context.Background(), 10*time.Millisecond, gate, func() (int, error) { <-unblock; return 1, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first probe=%v", err)
	}
	_, err = boundedProbe(context.Background(), time.Second, gate, func() (int, error) { return 2, nil })
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("second probe=%v duration=%s", err, time.Since(start))
	}
	close(unblock)
}

func TestStorageThresholds(t *testing.T) {
	maximum := int64(100)
	for _, test := range []struct {
		used int64
		want ThresholdState
	}{{0, ThresholdNormal}, {79, ThresholdNormal}, {80, ThresholdWarning}, {90, ThresholdStrongWarning}, {100, ThresholdLimitReached}} {
		if got := threshold(test.used, &maximum); got != test.want {
			t.Fatalf("used=%d got=%s want=%s", test.used, got, test.want)
		}
	}
}
