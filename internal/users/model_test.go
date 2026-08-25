package users

import (
	"strings"
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	for _, value := range []string{" Alice ", "alice", "ALICE"} {
		got, err := NormalizeUsername(value)
		if err != nil || got != "alice" {
			t.Fatalf("NormalizeUsername(%q)=%q,%v", value, got, err)
		}
	}
	if _, err := NormalizeUsername("  "); err == nil {
		t.Fatal("empty username accepted")
	}
	if _, err := NormalizeUsername(strings.Repeat("界", 65)); err == nil {
		t.Fatal("long username accepted")
	}
}
func TestPasswordPolicy(t *testing.T) {
	if err := ValidatePassword("1234567890"); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePassword("123456789"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := ValidatePassword(strings.Repeat("x", 1025)); err == nil {
		t.Fatal("oversized password accepted")
	}
	if err := ValidatePassword("          "); err != nil {
		t.Fatal("password whitespace was trimmed")
	}
}
