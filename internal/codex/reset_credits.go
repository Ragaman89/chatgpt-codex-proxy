package codex

import "time"

// ResetCredit is a read-only projection of one banked Codex limit reset. IDs
// are retained only inside the proxy and are deliberately not published to
// Home Assistant.
type ResetCredit struct {
	ID          string     `json:"id"`
	ResetType   string     `json:"reset_type"`
	Status      string     `json:"status"`
	GrantedAt   *time.Time `json:"granted_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
}

type ResetCreditsResponse struct {
	Credits []ResetCredit `json:"credits"`
}
