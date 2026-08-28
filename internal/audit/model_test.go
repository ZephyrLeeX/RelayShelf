package audit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPhase10MetadataIsExplicitlyAllowlisted(t *testing.T) {
	events := []Event{
		RuntimeSettingsUpdated(Actor{}, []string{"trashTtlHours"}),
		UserCreated(Actor{}, uuid.Must(uuid.NewV7()), "alice", false),
		UserDisabled(Actor{}, uuid.Must(uuid.NewV7())),
		UserPasswordReset(Actor{}, uuid.Must(uuid.NewV7())),
		UserDeleted(Actor{}, uuid.Must(uuid.NewV7()), "alice"),
	}
	for _, event := range events {
		encoded, err := json.Marshal(event.metadata)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(encoded))
		for _, forbidden := range []string{"password", "hash", "token", "cookie", "authorization", "body", "secret", "csrf", "storagekey"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("event=%s metadata=%s contains %q", event.Type, text, forbidden)
			}
		}
	}
}

// TestUserPasswordResetMetadataStaysEmpty pins the Phase 10 boundary: the
// password reset event carries no caller-supplied fields at all, so the only
// way to attach metadata to it would be editing this package.
func TestUserPasswordResetMetadataStaysEmpty(t *testing.T) {
	target := uuid.Must(uuid.NewV7())
	event := UserPasswordReset(Actor{UserID: uuid.Must(uuid.NewV7())}, target)
	if event.Type != EventUserPasswordReset || event.TargetType != "USER" || event.TargetID != target {
		t.Fatalf("event shape changed: %+v", event)
	}
	if len(event.metadata) != 0 {
		t.Fatalf("password reset metadata must remain empty, got %v", event.metadata)
	}
}
