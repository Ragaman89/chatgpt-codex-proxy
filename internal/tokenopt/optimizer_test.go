package tokenopt

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tiktoken-go/tokenizer"

	"chatgpt-codex-proxy/internal/config"
	"chatgpt-codex-proxy/internal/turn"
)

type fakeCompressor struct {
	calls int
	err   error
}

func (f *fakeCompressor) Compress(_ context.Context, texts []string, _ float64) ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	results := make([]string, len(texts))
	for index := range texts {
		results[index] = "condensed context"
	}
	return results, nil
}

func TestOptimizeOnlyCompressesSafeHistoricalTextAndCaches(t *testing.T) {
	t.Parallel()
	compressor := &fakeCompressor{}
	optimizer := newTestOptimizer(t, compressor)
	longText := strings.Repeat("historical natural language context ", 200)
	latest := "keep the latest user question exactly"
	request := turn.NormalizedRequest{Request: turn.Request{
		Input: []turn.InputItem{
			{Role: "developer", Content: []turn.ContentPart{{Type: "input_text", Text: longText}}},
			{Role: "user", Content: []turn.ContentPart{{Type: "input_text", Text: longText}}},
			{Role: "assistant", Content: []turn.ContentPart{{Type: "output_text", Text: longText}}},
			{Role: "assistant", Content: []turn.ContentPart{{Type: "output_text", Text: "```go\nfunc main() {}\n```"}}},
			{Type: "function_call_output", CallID: "call_1", OutputText: longText},
			{Role: "user", Content: []turn.ContentPart{{Type: "input_text", Text: latest}}},
		},
	}}

	optimized, report := optimizer.Optimize(context.Background(), request)
	if report.Status != "compressed" || report.CompressedTexts != 2 {
		t.Fatalf("report = %#v, want two compressed texts", report)
	}
	if got := optimized.Input[0].Content[0].Text; got != longText {
		t.Fatalf("developer text changed: %q", got)
	}
	if got := optimized.Input[1].Content[0].Text; got != "condensed context" {
		t.Fatalf("old user text = %q", got)
	}
	if got := optimized.Input[2].Content[0].Text; got != "condensed context" {
		t.Fatalf("old assistant text = %q", got)
	}
	if got := optimized.Input[3].Content[0].Text; got != "```go\nfunc main() {}\n```" {
		t.Fatalf("code changed: %q", got)
	}
	if got := optimized.Input[4].OutputText; got != longText {
		t.Fatalf("tool output changed: %q", got)
	}
	if got := optimized.Input[5].Content[0].Text; got != latest {
		t.Fatalf("latest question changed: %q", got)
	}
	if len(optimized.ContextManagement) != 1 || optimized.ContextManagement[0].CompactThreshold != 10000 {
		t.Fatalf("context management = %#v", optimized.ContextManagement)
	}

	_, cachedReport := optimizer.Optimize(context.Background(), request)
	if cachedReport.CacheHits != 2 || compressor.calls != 1 {
		t.Fatalf("cached report = %#v, calls = %d", cachedReport, compressor.calls)
	}
}

func TestOptimizeFailsOpenWhenCompressorIsUnavailable(t *testing.T) {
	t.Parallel()
	compressor := &fakeCompressor{err: errors.New("offline")}
	optimizer := newTestOptimizer(t, compressor)
	longText := strings.Repeat("historical context ", 300)
	request := turn.NormalizedRequest{Request: turn.Request{Input: []turn.InputItem{
		{Role: "user", Content: []turn.ContentPart{{Type: "input_text", Text: longText}}},
		{Role: "assistant", Content: []turn.ContentPart{{Type: "output_text", Text: "answer"}}},
		{Role: "user", Content: []turn.ContentPart{{Type: "input_text", Text: "latest"}}},
	}}}

	optimized, report := optimizer.Optimize(context.Background(), request)
	if report.Status != "compressor_unavailable" || report.Error == nil {
		t.Fatalf("report = %#v", report)
	}
	if got := optimized.Input[0].Content[0].Text; got != longText {
		t.Fatalf("text changed during fail-open: %q", got)
	}
	if len(optimized.ContextManagement) != 1 {
		t.Fatalf("automatic compaction missing: %#v", optimized.ContextManagement)
	}
}

func newTestOptimizer(t *testing.T, backend compressor) *Optimizer {
	t.Helper()
	codec, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		t.Fatal(err)
	}
	return &Optimizer{
		cfg: config.TokenOptimizationConfig{
			Enabled:              true,
			CompressorTimeout:    time.Second,
			MinRequestTokens:     1,
			MinTextTokens:        20,
			TargetRatio:          0.5,
			CacheEntries:         10,
			AutoCompactThreshold: 10000,
		},
		codec:      codec,
		compressor: backend,
		cache:      newLRUCache(10),
	}
}
