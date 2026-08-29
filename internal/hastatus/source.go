package hastatus

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"chatgpt-codex-proxy/internal/accountmanager"
	"chatgpt-codex-proxy/internal/accounts"
	"chatgpt-codex-proxy/internal/codex"
	"chatgpt-codex-proxy/internal/config"
)

const (
	fiveHourWindowDuration = 5 * time.Hour
	weeklyWindowMinimum    = 6 * 24 * time.Hour
)

type State struct {
	FiveHourRemainingPercent *float64   `json:"five_hour_remaining_percent"`
	FiveHourUsedPercent      *float64   `json:"five_hour_used_percent"`
	FiveHourResetAt          *time.Time `json:"five_hour_reset_at"`
	FiveHourWindowSeconds    *int       `json:"five_hour_window_seconds"`

	WeeklyRemainingPercent *float64   `json:"weekly_remaining_percent"`
	WeeklyUsedPercent      *float64   `json:"weekly_used_percent"`
	WeeklyResetAt          *time.Time `json:"weekly_reset_at"`
	WeeklyWindowSeconds    *int       `json:"weekly_window_seconds"`
	QuotaFresh             bool       `json:"quota_fresh"`
	QuotaFetchedAt         *time.Time `json:"quota_fetched_at"`

	AvailableResetCount  *int        `json:"available_reset_count"`
	ResetExpirations     []time.Time `json:"reset_expirations"`
	NextResetExpiry      *time.Time  `json:"next_reset_expiry"`
	ResetDetailsComplete bool        `json:"reset_details_complete"`

	CompressionConfigured  bool      `json:"compression_configured"`
	CompressorReachable    bool      `json:"compressor_reachable"`
	CompressionActive      bool      `json:"compression_active"`
	CompressionMinTokens   int       `json:"compression_min_tokens"`
	CompressionTargetRatio float64   `json:"compression_target_ratio"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type Source struct {
	accounts   *accounts.Service
	manager    *accountmanager.AccountManager
	codex      *codex.HTTPClient
	cfg        config.Config
	logger     *slog.Logger
	httpClient *http.Client
}

func NewSource(cfg config.Config, logger *slog.Logger, accountService *accounts.Service, manager *accountmanager.AccountManager, client *codex.HTTPClient) *Source {
	return &Source{
		accounts:   accountService,
		manager:    manager,
		codex:      client,
		cfg:        cfg,
		logger:     logger,
		httpClient: &http.Client{Timeout: cfg.TokenOptimization.CompressorTimeout},
	}
}

func (s *Source) Snapshot(ctx context.Context) State {
	now := time.Now().UTC()
	state := State{
		CompressionConfigured:  s.cfg.TokenOptimization.Enabled && s.cfg.TokenOptimization.CompressorURL != "",
		CompressionMinTokens:   s.cfg.TokenOptimization.MinRequestTokens,
		CompressionTargetRatio: s.cfg.TokenOptimization.TargetRatio,
		UpdatedAt:              now,
	}
	state.CompressorReachable = s.compressorReachable(ctx)
	state.CompressionActive = state.CompressionConfigured && state.CompressorReachable

	records, err := s.accounts.List()
	if err != nil {
		s.logger.Warn("Home Assistant quota account listing failed", "error", err)
		return state
	}
	record, ok := preferredAccount(records)
	if !ok {
		return state
	}

	updated, quota, err := s.manager.GetUsage(ctx, record.ID, false)
	if err != nil {
		s.logger.Warn("Home Assistant quota refresh failed; publishing cached snapshot", "error", err)
		quota = record.CachedQuota
	} else {
		record = updated
		state.QuotaFresh = true
	}
	if quota == nil {
		return state
	}

	fetchedAt := quota.FetchedAt.UTC()
	if !fetchedAt.IsZero() {
		state.QuotaFetchedAt = &fetchedAt
	}
	if window := fiveHourWindow(quota); window != nil {
		state.FiveHourUsedPercent = cloneFloat(window.UsedPercent)
		if window.UsedPercent != nil {
			remaining := remainingPercent(*window.UsedPercent)
			state.FiveHourRemainingPercent = &remaining
		}
		state.FiveHourResetAt = cloneTime(window.ResetAt)
		state.FiveHourWindowSeconds = cloneInt(window.LimitWindowSeconds)
	}
	if window := weeklyWindow(quota); window != nil {
		state.WeeklyUsedPercent = cloneFloat(window.UsedPercent)
		if window.UsedPercent != nil {
			remaining := remainingPercent(*window.UsedPercent)
			state.WeeklyRemainingPercent = &remaining
		}
		state.WeeklyResetAt = cloneTime(window.ResetAt)
		state.WeeklyWindowSeconds = cloneInt(window.LimitWindowSeconds)
	}
	if quota.RateLimitResetCredits != nil {
		count := max(quota.RateLimitResetCredits.AvailableCount, 0)
		state.AvailableResetCount = &count
		state.ResetDetailsComplete = count == 0
	}
	if state.AvailableResetCount != nil && *state.AvailableResetCount > 0 {
		details, detailErr := s.codex.GetResetCredits(ctx, record)
		if detailErr != nil {
			s.logger.Warn("Home Assistant reset-credit detail refresh failed", "error", detailErr)
			return state
		}
		availableRows := 0
		for _, credit := range details.Credits {
			if credit.Status != "available" {
				continue
			}
			availableRows++
			if credit.ExpiresAt != nil && credit.ExpiresAt.After(now) {
				state.ResetExpirations = append(state.ResetExpirations, credit.ExpiresAt.UTC())
			}
		}
		slices.SortFunc(state.ResetExpirations, func(a, b time.Time) int { return a.Compare(b) })
		if len(state.ResetExpirations) > 0 {
			next := state.ResetExpirations[0]
			state.NextResetExpiry = &next
		}
		state.ResetDetailsComplete = availableRows == *state.AvailableResetCount
	}
	return state
}

func fiveHourWindow(quota *accounts.QuotaSnapshot) *accounts.RateLimitWindow {
	if quota == nil {
		return nil
	}
	for _, candidate := range rateLimitWindows(quota) {
		if candidate.LimitWindowSeconds == nil {
			continue
		}
		windowDuration := time.Duration(*candidate.LimitWindowSeconds) * time.Second
		if windowDuration == fiveHourWindowDuration {
			return candidate
		}
	}
	return nil
}

func (s *Source) compressorReachable(ctx context.Context) bool {
	if !s.cfg.TokenOptimization.Enabled || s.cfg.TokenOptimization.CompressorURL == "" {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.TokenOptimization.CompressorURL+"/readyz", nil)
	if err != nil {
		return false
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func preferredAccount(records []accounts.Record) (accounts.Record, bool) {
	for _, record := range records {
		if record.Status == accounts.StatusActive && record.Token.AccessToken != "" {
			return record, true
		}
	}
	return accounts.Record{}, false
}

func weeklyWindow(quota *accounts.QuotaSnapshot) *accounts.RateLimitWindow {
	if quota == nil {
		return nil
	}
	var longest *accounts.RateLimitWindow
	for _, candidate := range rateLimitWindows(quota) {
		if candidate.LimitWindowSeconds == nil {
			continue
		}
		if time.Duration(*candidate.LimitWindowSeconds)*time.Second >= weeklyWindowMinimum {
			return candidate
		}
		if longest == nil || longest.LimitWindowSeconds == nil || *candidate.LimitWindowSeconds > *longest.LimitWindowSeconds {
			longest = candidate
		}
	}
	return longest
}

func rateLimitWindows(quota *accounts.QuotaSnapshot) []*accounts.RateLimitWindow {
	windows := []*accounts.RateLimitWindow{&quota.RateLimit}
	if quota.SecondaryRateLimit != nil {
		windows = append(windows, quota.SecondaryRateLimit)
	}
	return windows
}

func remainingPercent(usedPercent float64) float64 {
	return min(max(100-usedPercent, 0), 100)
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
