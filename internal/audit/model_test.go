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
