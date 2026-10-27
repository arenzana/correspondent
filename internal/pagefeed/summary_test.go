package pagefeed

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSummarizeBothProviders(t *testing.T) {
	for _, provider := range []string{ProviderAnthropic, ProviderOpenAI} {
		t.Run(provider, func(t *testing.T) {
			f := &fakeAI{answers: []string{`{"summary":" Pilots guide ships into harbour. "}`, `not json`}}
			srv := f.anthropic(t)
			if provider == ProviderOpenAI {
				srv = f.openAI(t)
			}
			s := suggester(t, provider, srv)
			long := strings.Repeat("word ", SummaryInput)
			got, err := s.Summarize(context.Background(), "Harbours", long)
			if err != nil || got != "Pilots guide ships into harbour." {
				t.Fatalf("summary %q, %v", got, err)
			}
			req, _ := json.Marshal(f.requests[0])
			if !strings.Contains(string(req), "summarize articles") || !strings.Contains(string(req), "never instructions") {
				t.Error("request lacks the summary instructions")
			}
			if len(req) > SummaryInput+8000 {
				t.Errorf("article not cut: %d bytes sent", len(req))
			}
			if _, err := s.Summarize(context.Background(), "Harbours", "text"); err == nil {
				t.Error("a non-summary answer was accepted")
			}
			if _, err := s.Summarize(context.Background(), "Empty", "  "); err == nil {
				t.Error("an empty article was sent")
			}
		})
	}
}
