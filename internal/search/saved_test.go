package search

import (
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/google/uuid"
	"testing"
)

func TestSavedSearchConditionValidation(t *testing.T) {
	base := httpapi.SearchConditions{Time: "all", Timezone: "UTC", TagIds: []uuid.UUID{}}
	if !validConditions(base) {
		t.Fatal("empty search must be saveable")
	}
	for _, mutate := range []func(*httpapi.SearchConditions){
		func(c *httpapi.SearchConditions) { c.Q = "x" },
		func(c *httpapi.SearchConditions) { c.Type = "UNSUPPORTED" },
		func(c *httpapi.SearchConditions) { c.Lifecycle = "TRASH" },
		func(c *httpapi.SearchConditions) { c.Time = "old" },
		func(c *httpapi.SearchConditions) { c.Timezone = "unknown" },
		func(c *httpapi.SearchConditions) { c.TagIds = []uuid.UUID{uuid.Nil} },
		func(c *httpapi.SearchConditions) { c.From = "2026-09-01T00:00" },
		func(c *httpapi.SearchConditions) {
			c.Time = "custom"
			c.From = "2026-02-30T00:00"
			c.To = "2026-03-01T00:00"
		},
		func(c *httpapi.SearchConditions) { c.Time = "custom"; c.From = "2026-09-01T00:00"; c.To = c.From },
	} {
		c := base
		mutate(&c)
		if validConditions(c) {
			t.Fatalf("accepted invalid conditions: %+v", c)
		}
	}
	for _, zone := range []string{"UTC", "Asia/Shanghai"} {
		c := base
		c.Time = "custom"
		c.Timezone = httpapi.SearchConditionsTimezone(zone)
		c.From = "2026-09-01T00:00"
		c.To = "2026-09-02T00:00"
		if !validConditions(c) {
			t.Fatalf("rejected valid date in %s", zone)
		}
	}
}
