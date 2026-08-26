package search

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSearchResultProjectionIsBoundedWithoutBoundingMatch(t *testing.T) {
	querySQL, _ := buildQuery(uuid.Must(uuid.NewV7()), Query{Tokens: []string{"latebodyneedle"}, Limit: 30}, time.Now())
	if !strings.Contains(querySQL, "left(m.body_plaintext, 16385)") {
		t.Fatal("candidate result does not use the bounded body projection")
	}
	if strings.Contains(querySQL, "m.owner_id, m.body_plaintext, m.body_format") {
		t.Fatal("candidate result still projects the complete body")
	}
	if !strings.Contains(querySQL, "m.body_plaintext ILIKE") {
		t.Fatal("body matching no longer checks the complete body column")
	}
}
