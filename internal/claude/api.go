package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/version"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	usagePath      = "/api/oauth/usage"
	betaHeader     = "oauth-2025-04-20"

	// maxResponseBytes caps the response body we'll read from the Anthropic API
	// to prevent a malicious or misbehaving server from exhausting process memory.
	maxResponseBytes = 1 << 20 // 1 MiB
)

// UsageResponse mirrors the Anthropic OAuth usage API response.
//
// The legacy per-window fields are pointers because the API sends null for
// windows that do not apply to the account (e.g. seven_day_sonnet once the
// per-model weekly limit moved into Limits).
type UsageResponse struct {
	FiveHour       *WindowData    `json:"five_hour"`
	SevenDay       *WindowData    `json:"seven_day"`
	SevenDayOAuth  *WindowData    `json:"seven_day_oauth_apps"`
	SevenDayOpus   *WindowData    `json:"seven_day_opus"`
	SevenDaySonnet *WindowData    `json:"seven_day_sonnet"`
	ExtraUsage     ExtraUsageData `json:"extra_usage"`
	// Limits is the newer, self-describing list of limits. When present it is
	// the source of truth for weekly windows, including model-scoped ones.
	Limits []LimitData `json:"limits"`
}

// Limit kinds understood by convertUsage.
const (
	limitKindWeeklyAll    = "weekly_all"
	limitKindWeeklyScoped = "weekly_scoped"
)

// LimitData is one entry of the usage API's limits array.
type LimitData struct {
	Kind     string      `json:"kind"`      // e.g. "weekly_all", "weekly_scoped"
	Group    string      `json:"group"`     // e.g. "weekly"
	Percent  float64     `json:"percent"`   // 0–100
	ResetsAt string      `json:"resets_at"` // ISO 8601
	Scope    *LimitScope `json:"scope"`
}

// LimitScope narrows a limit to a subset of usage (currently a model).
type LimitScope struct {
	Model *LimitScopeModel `json:"model"`
}

// LimitScopeModel identifies the model a scoped limit applies to.
type LimitScopeModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// WindowData holds the utilization fraction and reset time for one rate window.
type WindowData struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"` // ISO 8601
}

// ExtraUsageData holds pay-as-you-go billing data.
type ExtraUsageData struct {
	IsEnabled    bool    `json:"is_enabled"`
	MonthlyLimit float64 `json:"monthly_limit"`
	UsedCredits  float64 `json:"used_credits"`
	Utilization  float64 `json:"utilization"`
	Currency     string  `json:"currency"`
}

// APIClient fetches usage data from the Anthropic OAuth API.
type APIClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAPIClient creates an APIClient targeting the given base URL.
// Pass http.DefaultClient for production use; pass a custom client for tests.
func NewAPIClient(baseURL string, httpClient *http.Client) *APIClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &APIClient{baseURL: baseURL, httpClient: httpClient}
}

// FetchUsage calls GET /api/oauth/usage with the given access token.
// Returns a domain error on any HTTP or parsing failure.
func (c *APIClient) FetchUsage(ctx context.Context, accessToken string) (*UsageResponse, error) {
	url := c.baseURL + usagePath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to create usage request", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("User-Agent", "claude-code/"+version.ClaudeCLIVersion())
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("usage request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, apierrors.NewAuthError("Claude token is invalid or expired", fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("usage API returned an unexpected status (HTTP %d)", resp.StatusCode)
		if resp.StatusCode == http.StatusTooManyRequests {
			msg = "Claude API rate limit exceeded (HTTP 429)"
		}

		apiErr := apierrors.NewAPIError(msg, fmt.Errorf("HTTP %d", resp.StatusCode))
		apiErr.StatusCode = resp.StatusCode

		if retryAfter, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			apiErr.RetryAfter = retryAfter
			if resp.StatusCode == http.StatusTooManyRequests {
				// Round to nearest second to avoid messy fractions in user-facing output.
				rounded := retryAfter.Round(time.Second)
				apiErr.Message = fmt.Sprintf("Claude API rate limit exceeded (HTTP 429), retry after %v", rounded)
			}
		}

		return nil, apiErr
	}

	var usage UsageResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&usage); err != nil {
		return nil, apierrors.NewAPIError("failed to parse usage response", err)
	}
	normalizeUsage(&usage)
	return &usage, nil
}

func normalizeUsage(usage *UsageResponse) {
	for _, w := range []*WindowData{usage.FiveHour, usage.SevenDay, usage.SevenDayOAuth, usage.SevenDayOpus, usage.SevenDaySonnet} {
		if w != nil {
			w.Utilization = normalizeUtilization(w.Utilization)
		}
	}
	usage.ExtraUsage.Utilization = normalizeUtilization(usage.ExtraUsage.Utilization)
	if shouldNormalizeExtraUsageAmounts(usage.ExtraUsage) {
		usage.ExtraUsage.MonthlyLimit /= 100
		usage.ExtraUsage.UsedCredits /= 100
	}
}

func normalizeUtilization(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		value = value / 100
	}
	if value > 1 {
		return 1
	}
	return value
}

func shouldNormalizeExtraUsageAmounts(extra ExtraUsageData) bool {
	if extra.MonthlyLimit <= 0 || extra.UsedCredits < 0 {
		return false
	}
	// monthly_limit arrives as whole minor units (cents) for every account
	// currency we've observed, so shape alone — not currency — is the signal.
	// used_credits is runtime token accounting and may carry fractional cents.
	return isWholeNumber(extra.MonthlyLimit) && extra.MonthlyLimit >= 1000
}

func isWholeNumber(value float64) bool {
	return math.Abs(value-math.Round(value)) < 1e-9
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := when.Sub(now)
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}
