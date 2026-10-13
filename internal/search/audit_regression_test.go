package search

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
)

func TestSavedSearchRequiredJSONFields(t *testing.T) {
	base := httpapi.SearchConditions{Time: "all", Timezone: "UTC", TagIds: []uuid.UUID{}}
	raw, _ := json.Marshal(base)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for key, value := range fields {
		malformedValues := []json.RawMessage{nil, []byte("null"), []byte("{}"), []byte("42"), []byte(`[null]`)}
		if key == "tagIds" {
			malformedValues = append(malformedValues, []byte(`"invalid"`))
		}
		for _, malformed := range malformedValues {
			t.Run(key+"/"+string(malformed), func(t *testing.T) {
				if malformed == nil {
					delete(fields, key)
				} else {
					fields[key] = malformed
				}
				conditions, _ := json.Marshal(fields)
				request := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"bad","conditions":`+string(conditions)+`}`))
				response := httptest.NewRecorder()
				// Nil repository: invalid requests must stop before touching storage.
				NewHandler(nil).CreateSavedSearch(response, request)
				if response.Code != 422 {
					t.Fatalf("accepted malformed request: %d %s", response.Code, response.Body.String())
				}
				update := httptest.NewRequest("PUT", "/", strings.NewReader(`{"name":"bad","conditions":`+string(conditions)+`}`))
				updateResponse := httptest.NewRecorder()
				NewHandler(nil).UpdateSavedSearch(updateResponse, update, uuid.Nil)
				if updateResponse.Code != 422 {
					t.Fatalf("accepted malformed update: %d", updateResponse.Code)
				}
				fields[key] = value
			})
		}
	}
}
