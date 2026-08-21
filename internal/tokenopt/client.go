package tokenopt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxCompressorResponseBytes = 16 << 20

type compressor interface {
	Compress(context.Context, []string, float64) ([]string, error)
}

type httpCompressor struct {
	endpoint string
	client   *http.Client
}

func newHTTPCompressor(baseURL string, timeout time.Duration) *httpCompressor {
	return &httpCompressor{
		endpoint: strings.TrimRight(baseURL, "/") + "/v1/compress",
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *httpCompressor) Compress(ctx context.Context, texts []string, ratio float64) ([]string, error) {
	payload, err := json.Marshal(map[string]any{
		"texts":        texts,
		"target_ratio": ratio,
	})
	if err != nil {
		return nil, fmt.Errorf("encode compressor request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create compressor request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call compressor: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCompressorResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read compressor response: %w", err)
	}
	if len(body) > maxCompressorResponseBytes {
		return nil, fmt.Errorf("compressor response exceeds %d bytes", maxCompressorResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("compressor returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Results []struct {
			CompressedText string `json:"compressed_text"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("decode compressor response: %w", err)
	}
	if len(decoded.Results) != len(texts) {
		return nil, fmt.Errorf("compressor returned %d results for %d texts", len(decoded.Results), len(texts))
	}
	results := make([]string, len(decoded.Results))
	for index, result := range decoded.Results {
		results[index] = strings.TrimSpace(result.CompressedText)
		if results[index] == "" {
			return nil, fmt.Errorf("compressor returned an empty result at index %d", index)
		}
	}
	return results, nil
}
