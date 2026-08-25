package migrations

import "embed"

// Files is the immutable schema authority compiled into the binary.
// Existing numbered migrations are released history: never edit them. Add the
// next numbered migration for every schema change.
//
//go:embed *.sql
var Files embed.FS
