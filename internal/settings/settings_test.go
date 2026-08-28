package settings

import "testing"

func TestSettingsValidationAndChangedFieldAllowlist(t *testing.T) {
	base := Settings{TemporaryTTLHours: 72, TrashTTLHours: 168, MaxFileSizeBytes: 2 << 30, AuditRetentionDays: 90, UploadRetentionHours: 24}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := base
	invalid.AuditRetentionDays = 0
	if err := invalid.Validate(); err != ErrValidation {
		t.Fatalf("validation=%v", err)
	}
	changed := base
	changed.TrashTTLHours = 24
	changed.UploadRetentionHours = 12
	fields := changedFields(base, changed)
	if len(fields) != 2 || fields[0] != "trashTtlHours" || fields[1] != "uploadRetentionHours" {
		t.Fatalf("fields=%v", fields)
	}
}
