package main

import "testing"

func TestOperatorConfirmationRequiresExplicitYes(t *testing.T) {
	const name = "RELAYSHELF_TEST_CONFIRMATION"
	for _, test := range []struct {
		value string
		want  bool
	}{{"", false}, {"true", false}, {"1", false}, {"no", false}, {"yes", true}, {" YES ", true}} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv(name, test.value)
			if got := operatorConfirmed(name); got != test.want {
				t.Fatalf("operatorConfirmed(%q)=%v want=%v", test.value, got, test.want)
			}
		})
	}
}

func TestSecurityGateReadyOnlyAfterAutomatedAndManualChecks(t *testing.T) {
	for _, test := range []struct {
		name             string
		failures, manual int
		want             bool
	}{
		{"automated failure", 1, 0, false},
		{"manual pending", 0, 1, false},
		{"all satisfied", 0, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := securityGateReady(test.failures, test.manual); got != test.want {
				t.Fatalf("ready=%v want=%v", got, test.want)
			}
		})
	}
}
