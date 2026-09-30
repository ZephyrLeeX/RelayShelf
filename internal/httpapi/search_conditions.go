package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// UnmarshalJSON enforces the existing OpenAPI required fields, including their
// JSON types. Generated Go zero values cannot distinguish null/missing values
// from valid empty strings and false. This also makes historical malformed
// saved rows fail decoding, so the saved-search reader marks them invalid.
func (c *SearchConditions) UnmarshalJSON(raw []byte) error {
	type conditions SearchConditions
	var decoded conditions
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	*c = SearchConditions(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	for _, key := range []string{"q", "lifecycle", "favorite", "tagIds", "type", "time", "from", "to", "timezone"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("search conditions require non-null %s", key)
		}
	}
	return nil
}
