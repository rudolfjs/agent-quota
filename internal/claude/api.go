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

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/version"
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
type UsageResponse struct {
	FiveHour       WindowData     `json:"five_hour"`
	SevenDay       WindowData     `json:"seven_day"`
	SevenDayOAuth  WindowData     `json:"seven_day_oauth_apps"`
	SevenDayOpus   WindowData     `json:"seven_day_opus"`
	SevenDaySonnet WindowData     `json:"seven_day_sonnet"`
	ExtraUsage     ExtraUsageData `json:"extra_usage"`
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
		apiErr := apierrors.NewAPIError(
			"usage API returned an unexpected status",
			fmt.Errorf("HTTP %d", resp.StatusCode),
		)
		apiErr.StatusCode = resp.StatusCode
		if retryAfter, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			apiErr.RetryAfter = retryAfter
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
	usage.FiveHour.Utilization = normalizeUtilization(usage.FiveHour.Utilization)
	usage.SevenDay.Utilization = normalizeUtilization(usage.SevenDay.Utilization)
	usage.SevenDayOAuth.Utilization = normalizeUtilization(usage.SevenDayOAuth.Utilization)
	usage.SevenDayOpus.Utilization = normalizeUtilization(usage.SevenDayOpus.Utilization)
	usage.SevenDaySonnet.Utilization = normalizeUtilization(usage.SevenDaySonnet.Utilization)
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
	if extra.Currency != "" && !strings.EqualFold(extra.Currency, "USD") {
		return false
	}
	return isWholeNumber(extra.MonthlyLimit) && isWholeNumber(extra.UsedCredits) && extra.MonthlyLimit >= 1000
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
