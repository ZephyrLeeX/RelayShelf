package messages

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateHashCanonicalizesTagSetButPreservesBody(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	base := CreateCommand{Body: " x ", BodyFormat: Text, Lifecycle: Temporary, TagIDs: []uuid.UUID{a, b}}
	reordered := base
	reordered.TagIDs = []uuid.UUID{b, a}
	if hashCreate(base) != hashCreate(reordered) {
		t.Fatal("tag order changed semantic hash")
	}
	changed := base
	changed.Body = "x"
	if hashCreate(base) == hashCreate(changed) {
		t.Fatal("body whitespace was normalized")
	}
}
func TestIdempotencyKeyValidation(t *testing.T) {
	if !validIdempotencyKey("request-1") || validIdempotencyKey("") || validIdempotencyKey(strings.Repeat("x", 129)) || validIdempotencyKey("bad\nkey") {
		t.Fatal("key validation mismatch")
	}
}
