package tags

import (
	"strings"
	"testing"
)

func TestNormalizeTag(t *testing.T) {
	display, normalized, color, err := normalize("  标签A  ", "#3b82f6")
	if err != nil || display != "标签A" || normalized != "标签a" || color != "#3B82F6" {
		t.Fatalf("%q %q %q %v", display, normalized, color, err)
	}
	for _, tc := range []struct{ name, color string }{{"   ", "#FFFFFF"}, {"ok", "red"}, {strings.Repeat("界", 65), "#000000"}} {
		if _, _, _, err = normalize(tc.name, tc.color); err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
}
