package tokenopt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/tiktoken-go/tokenizer"

	"chatgpt-codex-proxy/internal/config"
	"chatgpt-codex-proxy/internal/turn"
)

type Report struct {
	Status          string
	OriginalTokens  int
	OptimizedTokens int
	EligibleTexts   int
	CompressedTexts int
	CacheHits       int
	Error           error
}

type Optimizer struct {
	cfg        config.TokenOptimizationConfig
	codec      tokenizer.Codec
	compressor compressor
	cache      *lruCache
}

type candidate struct {
	itemIndex int
	partIndex int
	text      string
	cacheKey  string
}

func New(cfg config.TokenOptimizationConfig) (*Optimizer, error) {
	codec, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		return nil, fmt.Errorf("initialize tokenizer: %w", err)
	}
	optimizer := &Optimizer{
		cfg:   cfg,
		codec: codec,
		cache: newLRUCache(cfg.CacheEntries),
	}
	if cfg.CompressorURL != "" {
		optimizer.compressor = newHTTPCompressor(cfg.CompressorURL, cfg.CompressorTimeout)
	}
	return optimizer, nil
}

// Optimize applies learned compression only to old, natural-language message
// content. Instructions, the latest user request, tools, tool I/O, structured
// data, code, files, images and encrypted state are never modified.
func (o *Optimizer) Optimize(ctx context.Context, request turn.NormalizedRequest) (turn.NormalizedRequest, Report) {
	report := Report{Status: "disabled"}
	if o == nil || !o.cfg.Enabled {
		return request, report
	}

	report.OriginalTokens, report.Error = o.Count(request.Request)
	if report.Error != nil {
		report.Status = "token_count_failed"
		return request, report
	}
	if report.OriginalTokens < o.cfg.MinRequestTokens {
		report.Status = "below_threshold"
		report.OptimizedTokens = report.OriginalTokens
		return withAutoCompaction(request, o.cfg.AutoCompactThreshold), report
	}
	if o.compressor == nil {
		report.Status = "compressor_unconfigured"
		report.OptimizedTokens = report.OriginalTokens
		return withAutoCompaction(request, o.cfg.AutoCompactThreshold), report
	}

	optimized := cloneRequest(request)
	candidates := o.candidates(optimized)
	report.EligibleTexts = len(candidates)
	if len(candidates) == 0 {
		report.Status = "no_safe_candidates"
		report.OptimizedTokens = report.OriginalTokens
		return withAutoCompaction(optimized, o.cfg.AutoCompactThreshold), report
	}

	pending := make([]candidate, 0, len(candidates))
	pendingTexts := make([]string, 0, len(candidates))
	for _, item := range candidates {
		if value, ok := o.cache.get(item.cacheKey); ok {
			optimized.Input[item.itemIndex].Content[item.partIndex].Text = value
			report.CacheHits++
			report.CompressedTexts++
			continue
		}
		pending = append(pending, item)
		pendingTexts = append(pendingTexts, item.text)
	}

	if len(pending) > 0 {
		compressed, err := o.compressor.Compress(ctx, pendingTexts, o.cfg.TargetRatio)
		if err != nil {
			report.Status = "compressor_unavailable"
			report.Error = err
			report.OptimizedTokens = report.OriginalTokens
			return withAutoCompaction(request, o.cfg.AutoCompactThreshold), report
		}
		for index, value := range compressed {
			item := pending[index]
			originalCount, _ := o.codec.Count(item.text)
			compressedCount, _ := o.codec.Count(value)
			// A learned compressor may occasionally expand short or unusual text.
			// Preserve the original in that case.
			if compressedCount <= 0 || compressedCount >= originalCount {
				continue
			}
			optimized.Input[item.itemIndex].Content[item.partIndex].Text = value
			o.cache.put(item.cacheKey, value)
			report.CompressedTexts++
		}
	}

	optimized = withAutoCompaction(optimized, o.cfg.AutoCompactThreshold)
	report.OptimizedTokens, report.Error = o.Count(optimized.Request)
	if report.Error != nil {
		report.Status = "token_count_failed"
		return request, report
	}
	if report.CompressedTexts == 0 {
		report.Status = "unchanged"
	} else {
		report.Status = "compressed"
	}
	return optimized, report
}

func (o *Optimizer) Count(request turn.Request) (int, error) {
	segments := make([]string, 0, len(request.Input)+len(request.Tools)+1)
	appendText := func(value string) {
		if value = strings.TrimSpace(value); value != "" {
			segments = append(segments, value)
		}
	}
	appendText(request.Instructions)
	for _, item := range request.Input {
		appendText(item.Role)
		appendText(item.Type)
		appendText(item.Name)
		appendText(item.Input)
		appendText(item.Arguments)
		appendText(item.OutputText)
		appendText(item.EncryptedContent)
		for _, part := range slices.Concat(item.Content, item.OutputContent) {
			appendText(part.Text)
			appendText(part.FileURL)
			appendText(part.FileID)
			appendText(part.ImageURL)
		}
		for _, part := range item.Summary {
			appendText(part.Text)
		}
	}
	for _, tool := range request.Tools {
		encoded, err := json.Marshal(tool)
		if err != nil {
			return 0, fmt.Errorf("encode tool for token count: %w", err)
		}
		appendText(string(encoded))
	}
	if request.Text != nil {
		encoded, err := json.Marshal(request.Text)
		if err != nil {
			return 0, fmt.Errorf("encode output format for token count: %w", err)
		}
		appendText(string(encoded))
	}
	if len(segments) == 0 {
		return 0, nil
	}
	return o.codec.Count(strings.Join(segments, "\n"))
}

func (o *Optimizer) candidates(request turn.NormalizedRequest) []candidate {
	lastUser := -1
	for index, item := range request.Input {
		if item.Role == "user" {
			lastUser = index
		}
	}
	if lastUser <= 0 {
		return nil
	}

	var out []candidate
	for itemIndex := 0; itemIndex < lastUser; itemIndex++ {
		item := request.Input[itemIndex]
		if !safeMessageItem(item) {
			continue
		}
		for partIndex, part := range item.Content {
			if !safeTextPart(part) || unsafeStructuredText(part.Text) {
				continue
			}
			count, err := o.codec.Count(part.Text)
			if err != nil || count < o.cfg.MinTextTokens {
				continue
			}
			out = append(out, candidate{
				itemIndex: itemIndex,
				partIndex: partIndex,
				text:      part.Text,
				cacheKey:  compressionCacheKey(part.Text, o.cfg.TargetRatio),
			})
		}
	}
	return out
}

func safeMessageItem(item turn.InputItem) bool {
	if item.Role != "user" && item.Role != "assistant" {
		return false
	}
	if item.Type != "" && item.Type != "message" {
		return false
	}
	return item.EncryptedContent == "" && item.CallID == "" && item.Name == "" &&
		item.Input == "" && item.Arguments == "" && item.OutputText == "" &&
		len(item.OutputContent) == 0 && len(item.Summary) == 0
}

func safeTextPart(part turn.ContentPart) bool {
	if part.PromptCacheBreakpoint != nil {
		return false
	}
	switch part.Type {
	case "", "text", "input_text", "output_text":
		return part.ImageURL == "" && part.FileURL == "" && part.FileData == "" && part.FileID == ""
	default:
		return false
	}
}

func unsafeStructuredText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.Contains(trimmed, "```") || strings.Contains(trimmed, "<tool") {
		return true
	}
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		return json.Valid([]byte(trimmed))
	}
	return false
}

func compressionCacheKey(text string, ratio float64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("llmlingua2:v1:%.4f\x00%s", ratio, text)))
	return hex.EncodeToString(sum[:])
}

func cloneRequest(request turn.NormalizedRequest) turn.NormalizedRequest {
	cloned := request
	cloned.Input = append([]turn.InputItem(nil), request.Input...)
	for index := range cloned.Input {
		cloned.Input[index].Content = append([]turn.ContentPart(nil), request.Input[index].Content...)
		cloned.Input[index].OutputContent = append([]turn.ContentPart(nil), request.Input[index].OutputContent...)
		cloned.Input[index].Summary = append([]turn.ReasoningPart(nil), request.Input[index].Summary...)
	}
	return cloned
}

func withAutoCompaction(request turn.NormalizedRequest, threshold int) turn.NormalizedRequest {
	if threshold <= 0 || len(request.ContextManagement) > 0 {
		return request
	}
	request.ContextManagement = []turn.ContextManagement{{Type: "compaction", CompactThreshold: threshold}}
	return request
}
