package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type UsageResponseRateLimit struct {
	Allowed         bool         `json:"allowed"`
	LimitReached    bool         `json:"limit_reached"`
	PrimaryWindow   *UsageWindow `json:"primary_window"`
	SecondaryWindow *UsageWindow `json:"secondary_window,omitempty"`
}

type UsageResponseCodeReviewRateLimit struct {
	Allowed       bool         `json:"allowed"`
	LimitReached  bool         `json:"limit_reached"`
	PrimaryWindow *UsageWindow `json:"primary_window"`
}

type UsageResponseCredits struct {
	HasCredits  *bool    `json:"has_credits,omitempty"`
	Unlimited   *bool    `json:"unlimited,omitempty"`
	Balance     *float64 `json:"balance,omitempty"`
	ActiveLimit *string  `json:"active_limit,omitempty"`
}

// UnmarshalJSON accepts both the numeric balance used by older responses and
// the decimal string returned by current Codex usage responses.
func (c *UsageResponseCredits) UnmarshalJSON(data []byte) error {
	type wireCredits struct {
		HasCredits  *bool           `json:"has_credits,omitempty"`
		Unlimited   *bool           `json:"unlimited,omitempty"`
		Balance     json.RawMessage `json:"balance,omitempty"`
		ActiveLimit *string         `json:"active_limit,omitempty"`
	}
	var wire wireCredits
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	c.HasCredits = wire.HasCredits
	c.Unlimited = wire.Unlimited
	c.ActiveLimit = wire.ActiveLimit
	if len(wire.Balance) == 0 || bytes.Equal(wire.Balance, []byte("null")) {
		return nil
	}
	var value float64
	if wire.Balance[0] == '"' {
		var text string
		if err := json.Unmarshal(wire.Balance, &text); err != nil {
			return fmt.Errorf("decode credits balance: %w", err)
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return fmt.Errorf("decode credits balance %q: %w", text, err)
		}
		value = parsed
	} else if err := json.Unmarshal(wire.Balance, &value); err != nil {
		return fmt.Errorf("decode credits balance: %w", err)
	}
	c.Balance = &value
	return nil
}

type UsageResponseResetCredits struct {
	AvailableCount int `json:"available_count"`
}

type UsageResponse struct {
	PlanType              string                            `json:"plan_type"`
	RateLimit             UsageResponseRateLimit            `json:"rate_limit"`
	CodeReviewRateLimit   *UsageResponseCodeReviewRateLimit `json:"code_review_rate_limit,omitempty"`
	Credits               *UsageResponseCredits             `json:"credits,omitempty"`
	RateLimitResetCredits *UsageResponseResetCredits        `json:"rate_limit_reset_credits,omitempty"`
}

type UsageWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int     `json:"limit_window_seconds"`
	ResetAt            int64   `json:"reset_at"`
}
