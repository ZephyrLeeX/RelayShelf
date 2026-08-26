package search

import (
	"strings"
	"unicode/utf8"
)

func tokenize(value string) ([]string, error) {
	if !utf8.ValidString(value) || len(value) > MaxQueryBytes {
		return nil, ErrQueryTooLong
	}
	fields := strings.Fields(value)
	return normalizeTokens(fields)
}

func normalizeTokens(fields []string) ([]string, error) {
	if len(fields) > MaxTokens {
		return nil, ErrTooManyTokens
	}
	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	totalBytes := 0
	for _, field := range fields {
		totalBytes += len(field)
		parts := strings.Fields(field)
		if !utf8.ValidString(field) || len(parts) != 1 || parts[0] != field {
			return nil, ErrValidation
		}
		count := utf8.RuneCountInString(field)
		if count < MinTokenRunes {
			return nil, ErrQueryTooShort
		}
		if count > MaxTokenRunes {
			return nil, ErrQueryTooLong
		}
		key := strings.ToLower(field)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, field)
	}
	if totalBytes+max(0, len(fields)-1) > MaxQueryBytes {
		return nil, ErrQueryTooLong
	}
	return result, nil
}

func likePattern(token string) string {
	var builder strings.Builder
	builder.Grow(len(token) + 2)
	builder.WriteByte('%')
	for _, char := range token {
		if char == '\\' || char == '%' || char == '_' {
			builder.WriteByte('\\')
		}
		builder.WriteRune(char)
	}
	builder.WriteByte('%')
	return builder.String()
}
