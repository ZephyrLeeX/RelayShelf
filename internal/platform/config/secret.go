// Package config loads immutable deployment configuration.
package config

import "encoding/json"

// Secret keeps bytes from being accidentally exposed by common formatters or
// encoders. Call Bytes only at the narrowly-scoped point a secret is needed.
type Secret struct{ value []byte }

func newSecret(value []byte) Secret { return Secret{value: append([]byte(nil), value...)} }

func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal("[REDACTED]") }
func (s Secret) Bytes() []byte              { return append([]byte(nil), s.value...) }
