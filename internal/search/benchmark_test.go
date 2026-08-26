//go:build integration && searchbench && !race

package search

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	benchmarkWarmups = 20
	benchmarkSamples = 200
)

type latencySummary struct {
	P50, P95, P99, Max time.Duration
}

func summarizeLatency(samples []time.Duration) latencySummary {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(left, right int) bool { return sorted[left] < sorted[right] })
	percentile := func(value int) time.Duration {
		index := (len(sorted)*value + 99) / 100
		if index < 1 {
			index = 1
		}
		return sorted[index-1]
	}
	return latencySummary{P50: percentile(50), P95: percentile(95), P99: percentile(99), Max: sorted[len(sorted)-1]}
}

func TestSearchBenchmark100k(t *testing.T) {
	dataset := seedTargetDataset(t)
	repository := NewPostgreSQLRepository(dataset.db)
	service := NewService(repository, fixedClock{dataset.now})
	lifecycle := "PERMANENT"
	queries := []struct {
		name  string
		query Query
	}{
		{"body", Query{Tokens: []string{"needlebody"}, Limit: 30}},
		{"filename", Query{Tokens: []string{"needlefile"}, Limit: 30}},
		{"multi_token_and", Query{Tokens: []string{"needlebody", "postgres"}, Limit: 30}},
		{"text_lifecycle_tag", Query{Tokens: []string{"needlebody"}, Lifecycle: &lifecycle, TagIDs: []uuid.UUID{dataset.filterTagA}, Limit: 30}},
	}
	for _, benchmark := range queries {
		t.Run(benchmark.name, func(t *testing.T) {
			for index := 0; index < benchmarkWarmups; index++ {
				if _, err := service.Search(context.Background(), dataset.userA, benchmark.query); err != nil {
					t.Fatal(err)
				}
			}
			samples := make([]time.Duration, 0, benchmarkSamples)
			for index := 0; index < benchmarkSamples; index++ {
				started := time.Now()
				if _, err := service.Search(context.Background(), dataset.userA, benchmark.query); err != nil {
					t.Fatal(err)
				}
				samples = append(samples, time.Since(started))
			}
			summary := summarizeLatency(samples)
			t.Logf("messages=%d attachments=%d tags=%d tag_relations=%d warmup=%d samples=%d p50=%s p95=%s p99=%s max=%s", targetMessageCount, targetAttachmentCount, targetTagCount, targetRelationCount, benchmarkWarmups, benchmarkSamples, summary.P50, summary.P95, summary.P99, summary.Max)
			if summary.P95 >= 500*time.Millisecond {
				t.Fatalf("P95=%s exceeds 500ms target", summary.P95)
			}
		})
	}
}
