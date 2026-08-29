package hastatus

import (
	"testing"
	"time"

	"chatgpt-codex-proxy/internal/accounts"
)

func TestFiveHourWindowFindsWindowInEitherSlot(t *testing.T) {
	t.Parallel()

	fiveHours := int((5 * time.Hour).Seconds())
	sevenDays := int((7 * 24 * time.Hour).Seconds())
	usedShort := 40.0
	usedWeekly := 73.0
	quota := &accounts.QuotaSnapshot{
		RateLimit:          accounts.RateLimitWindow{UsedPercent: &usedShort, LimitWindowSeconds: &fiveHours},
		SecondaryRateLimit: &accounts.RateLimitWindow{UsedPercent: &usedWeekly, LimitWindowSeconds: &sevenDays},
	}

	window := fiveHourWindow(quota)
	if window == nil || window.UsedPercent == nil || *window.UsedPercent != usedShort {
		t.Fatalf("fiveHourWindow() = %#v, want five-hour window", window)
	}

	quota.RateLimit, *quota.SecondaryRateLimit = *quota.SecondaryRateLimit, quota.RateLimit
	window = fiveHourWindow(quota)
	if window == nil || window.UsedPercent == nil || *window.UsedPercent != usedShort {
		t.Fatalf("fiveHourWindow() with secondary five-hour = %#v, want five-hour window", window)
	}
}

func TestFiveHourWindowDoesNotSubstituteAnotherWindow(t *testing.T) {
	t.Parallel()

	oneHour := int(time.Hour.Seconds())
	sevenDays := int((7 * 24 * time.Hour).Seconds())
	quota := &accounts.QuotaSnapshot{
		RateLimit:          accounts.RateLimitWindow{LimitWindowSeconds: &oneHour},
		SecondaryRateLimit: &accounts.RateLimitWindow{LimitWindowSeconds: &sevenDays},
	}

	if window := fiveHourWindow(quota); window != nil {
		t.Fatalf("fiveHourWindow() = %#v, want nil without a five-hour window", window)
	}
}

func TestWeeklyWindowFindsSevenDayWindowInEitherSlot(t *testing.T) {
	t.Parallel()

	fiveHours := 5 * 60 * 60
	sevenDays := 7 * 24 * 60 * 60
	usedShort := 40.0
	usedWeekly := 73.0
	quota := &accounts.QuotaSnapshot{
		RateLimit:          accounts.RateLimitWindow{UsedPercent: &usedShort, LimitWindowSeconds: &fiveHours},
		SecondaryRateLimit: &accounts.RateLimitWindow{UsedPercent: &usedWeekly, LimitWindowSeconds: &sevenDays},
	}

	window := weeklyWindow(quota)
	if window == nil || window.UsedPercent == nil || *window.UsedPercent != usedWeekly {
		t.Fatalf("weeklyWindow() = %#v, want seven-day window", window)
	}

	quota.RateLimit, *quota.SecondaryRateLimit = *quota.SecondaryRateLimit, quota.RateLimit
	window = weeklyWindow(quota)
	if window == nil || window.UsedPercent == nil || *window.UsedPercent != usedWeekly {
		t.Fatalf("weeklyWindow() with primary weekly = %#v, want seven-day window", window)
	}
}

func TestWeeklyWindowFallsBackToLongestKnownWindow(t *testing.T) {
	t.Parallel()

	oneHour := int(time.Hour.Seconds())
	fiveHours := int((5 * time.Hour).Seconds())
	quota := &accounts.QuotaSnapshot{
		RateLimit:          accounts.RateLimitWindow{LimitWindowSeconds: &oneHour},
		SecondaryRateLimit: &accounts.RateLimitWindow{LimitWindowSeconds: &fiveHours},
	}

	window := weeklyWindow(quota)
	if window == nil || window.LimitWindowSeconds == nil || *window.LimitWindowSeconds != fiveHours {
		t.Fatalf("weeklyWindow() = %#v, want longest window", window)
	}
}

func TestPreferredAccountExcludesDisabledAccounts(t *testing.T) {
	t.Parallel()

	record, ok := preferredAccount([]accounts.Record{
		{ID: "disabled", Status: accounts.StatusDisabled, Token: accounts.OAuthToken{AccessToken: "token"}},
		{ID: "active", Status: accounts.StatusActive, Token: accounts.OAuthToken{AccessToken: "token"}},
	})
	if !ok || record.ID != "active" {
		t.Fatalf("preferredAccount() = (%q, %v), want active", record.ID, ok)
	}
}
