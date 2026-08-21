package server

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"chatgpt-codex-proxy/internal/tokenopt"
	"chatgpt-codex-proxy/internal/turn"
)

func (a *App) optimizeRequest(c *gin.Context, endpoint string, request turn.NormalizedRequest) turn.NormalizedRequest {
	if a == nil || a.tokenOptimizer == nil {
		return request
	}
	optimized, report := a.tokenOptimizer.Optimize(c.Request.Context(), request)
	setOptimizationHeaders(c, report)
	attrs := []any{
		"endpoint", endpoint,
		"status", report.Status,
		"original_tokens", report.OriginalTokens,
		"optimized_tokens", report.OptimizedTokens,
		"eligible_texts", report.EligibleTexts,
		"compressed_texts", report.CompressedTexts,
		"cache_hits", report.CacheHits,
	}
	if report.Error != nil {
		attrs = append(attrs, "error", report.Error.Error())
		a.logger.Warn("token optimization degraded", attrs...)
	} else if report.Status == "compressed" {
		a.logger.Info("token optimization completed", attrs...)
	}
	return optimized
}

func setOptimizationHeaders(c *gin.Context, report tokenopt.Report) {
	if c == nil {
		return
	}
	c.Header("X-Token-Optimization", report.Status)
	if report.OriginalTokens > 0 {
		c.Header("X-Original-Input-Tokens", strconv.Itoa(report.OriginalTokens))
	}
	if report.OptimizedTokens > 0 {
		c.Header("X-Optimized-Input-Tokens", strconv.Itoa(report.OptimizedTokens))
	}
	if report.CacheHits > 0 {
		c.Header("X-Compression-Cache-Hits", strconv.Itoa(report.CacheHits))
	}
}
