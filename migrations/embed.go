package migrations

import "embed"

// Files is the immutable schema authority compiled into the binary.
//
//go:embed *.sql
var Files embed.FS
