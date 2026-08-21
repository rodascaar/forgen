package usage_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/storage"
	"github.com/rodascaar/forgen/internal/application/usage"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestSummarizeAggregatesByModel(t *testing.T) {
	records := []domain.UsageRecord{
		{Provider: "openai", Model: "gpt-5", InputTokens: 100, OutputTokens: 50},
		{Provider: "openai", Model: "gpt-5", InputTokens: 200, OutputTokens: 100},
		{Provider: "anthropic", Model: "claude-sonnet-4-5", InputTokens: 10, OutputTokens: 5},
	}
	summaries := usage.Summarize(records)
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(summaries))
	}
	// El modelo con más tokens va primero.
	if summaries[0].Model != "openai/gpt-5" {
		t.Fatalf("primer modelo = %q, want openai/gpt-5", summaries[0].Model)
	}
	if summaries[0].Requests != 2 || summaries[0].InputTokens != 300 || summaries[0].OutputTokens != 150 {
		t.Fatalf("summary gpt-5 = %+v", summaries[0])
	}
}

func TestUsageStoreRoundTrip(t *testing.T) {
	store := storage.NewJSONLUsageStore(t.TempDir() + "/usage.jsonl")
	service := usage.NewService(store, slog.Default())

	records := []domain.UsageRecord{
		{SessionID: "s1", Provider: "openai", Model: "gpt-5", Phase: "build", InputTokens: 10, OutputTokens: 5},
		{SessionID: "s1", Provider: "openai", Model: "gpt-5", Phase: "review", InputTokens: 20, OutputTokens: 8},
	}
	for _, record := range records {
		if err := service.Record(context.Background(), record); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	listed, err := service.List(context.Background(), 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("list = %d, want 2", len(listed))
	}
	// El más reciente va primero.
	if listed[0].Phase != "review" {
		t.Fatalf("primer registro = %+v, want review", listed[0])
	}
}
