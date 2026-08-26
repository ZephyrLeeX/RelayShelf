package search

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestTokenizeWhitespaceDedupAndUnicode(t *testing.T) {
	tokens, err := tokenize("postgres   docker\ncompose docker DOCKER 中文")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"postgres", "docker", "compose", "中文"}
	if len(tokens) != len(want) {
		t.Fatalf("tokens=%q want=%q", tokens, want)
	}
	for index := range want {
		if tokens[index] != want[index] {
			t.Fatalf("tokens=%q want=%q", tokens, want)
		}
	}
	if tokens, err = tokenize(" \t\n"); err != nil || len(tokens) != 0 {
		t.Fatalf("whitespace tokens=%q err=%v", tokens, err)
	}
}

func TestBuildQueryOnlyInterpolatesFixedFragments(t *testing.T) {
	owner := uuid.Must(uuid.NewV7())
	injection := `' OR 1=1 --`
	querySQL, args := buildQuery(owner, Query{Tokens: []string{injection}, Limit: 30}, time.Now())
	if strings.Contains(querySQL, injection) || !strings.Contains(querySQL, "ILIKE $3") {
		t.Fatalf("query interpolated user data: %s", querySQL)
	}
	if len(args) < 3 || args[2] != likePattern(injection) {
		t.Fatalf("parameter args=%#v", args)
	}
	filterOnlySQL, _ := buildQuery(owner, Query{Limit: 30}, time.Now())
	if strings.Contains(filterOnlySQL, "text_matches") {
		t.Fatalf("filter-only query contains text CTE: %s", filterOnlySQL)
	}
}

func TestTokenizeBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		want  error
	}{
		{"one ASCII rune", "a", ErrQueryTooShort},
		{"one Unicode rune", "中", ErrQueryTooShort},
		{"invalid UTF-8", string([]byte{0xff, 0xfe}), ErrQueryTooLong},
		{"query bytes", strings.Repeat("a", MaxQueryBytes+1), ErrQueryTooLong},
		{"token runes", strings.Repeat("界", MaxTokenRunes+1), ErrQueryTooLong},
		{"token count", strings.Repeat("aa ", MaxTokens) + "bb", ErrTooManyTokens},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := tokenize(test.query)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}
	if tokens, err := tokenize("中文"); err != nil || len(tokens) != 1 || utf8.RuneCountInString(tokens[0]) != 2 {
		t.Fatalf("two-rune query tokens=%q err=%v", tokens, err)
	}
}

func TestLikePatternEscapesLiteralMetacharacters(t *testing.T) {
	for input, want := range map[string]string{
		"100%":    `%100\%%`,
		"foo_bar": `%foo\_bar%`,
		`C:\data`: `%C:\\data%`,
		`%_\`:     `%\%\_\\%`,
	} {
		if got := likePattern(input); got != want {
			t.Fatalf("likePattern(%q)=%q want=%q", input, got, want)
		}
	}
}
