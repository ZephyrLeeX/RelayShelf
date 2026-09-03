package storage

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyCause(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "full", err: ErrFull, want: "NAS_FULL"},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: "NAS_TIMEOUT"},
		{name: "unavailable", err: errors.New("storage unavailable"), want: "NAS_UNAVAILABLE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyCause(tt.err); got != tt.want {
				t.Fatalf("classifyCause(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}
