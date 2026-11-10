package claude

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rudolfjs/agent-quota/internal/credential"
	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

// Claude implements provider.Provider for the Anthropic Claude API.
type Claude struct {
	credPath       string
	credentials    credential.Source
	backoffPath    string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	baseURL        string
	forcedReset    bool // set by ResetBackoff; suppresses backoff persistence on the next fetch
}

// Option configures a Claude provider instance.
type Option func(*Claude)

// WithCredentialsPath sets the path to the credentials JSON file.
func WithCredentialsPath(path string) Option {
	return func(c *Claude) { c.credPath = path }
}

// WithBackoffPath sets the path to the backoff state JSON file.
func WithBackoffPath(path string) Option {
	return func(c *Claude) { c.backoffPath = path }
}

// WithHTTPClient sets a custom HTTP client for API requests.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Claude) { c.httpClient = client }
}

// WithBaseURL sets the API base URL (useful for testing).
func WithBaseURL(url string) Option {
	return func(c *Claude) { c.baseURL = url }
}

// defaultHTTPClient is the HTTP client used when none is provided via WithHTTPClient.
// A 30-second timeout prevents hung API servers from blocking the process indefinitely.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

// New creates a Claude provider with the given options.
func New(opts ...Option) *Claude {
	c := &Claude{httpClient: defaultHTTPClient}
	for _, o := range opts {
		o(c)
	}
	// Set default credentials path only if not overridden by WithCredentialsPath.
	if c.credPath == "" {
		c.credentials.Keychain = defaultKeychain()
		path, err := DefaultCredentialsPath()
		c.credPath = path
		if c.defaultPathErr == nil {
			c.defaultPathErr = err
		}
	}
	if c.backoffPath == "" {
		path, err := defaultBackoffPath()
		c.backoffPath = path
		if c.defaultPathErr == nil {
			c.defaultPathErr = err
		}
	}
	c.credentials.Path = c.credPath
	c.credentials.Label = "Claude"
	return c
}

// Name returns "claude".
func (c *Claude) Name() string { return "claude" }

// Available reports whether Claude credentials are present and readable.
func (c *Claude) Available() bool {
	if c.defaultPathErr != nil {
		return false
	}
	return c.credentials.Available(func(data []byte) bool {
		creds, err := parseCredentials(data)
		return err == nil && creds.AccessToken != ""
	})
}

// ResetBackoff clears Claude's persisted local rate-limit cooldown.
// It also sets an internal flag so the next FetchQuota call skips
// re-persisting backoff state on 429 responses.
func (c *Claude) ResetBackoff() error {
	if c.defaultPathErr != nil {
		return apierrors.NewConfigError("cannot determine Claude configuration paths", c.defaultPathErr)
	}
	if err := clearBackoffState(c.backoffPath); err != nil {
		return apierrors.NewConfigError("failed to clear Claude rate-limit backoff state", err)
	}
	c.forcedReset = true
	return nil
}

// FetchQuota retrieves usage data from the Anthropic OAuth API.
func (c *Claude) FetchQuota(ctx context.Context) (provider.QuotaResult, error) {
	if c.defaultPathErr != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("cannot determine Claude configuration paths", c.defaultPathErr)
	}

	// Determine whether this fetch was triggered by an explicit user action
	// (--force flag or ctrl+r). When forced, we skip reading persisted backoff
	// and skip re-persisting backoff on 429 responses.
	forced := c.forcedReset || ctx.Value(provider.ForceRetryKey{}) != nil
	if forced {
		c.forcedReset = false // consume the flag
	}

	if !forced {
		backoffEnd := readBackoffState(c.backoffPath)
		if !backoffEnd.IsZero() && time.Now().Before(backoffEnd) {
			remaining := time.Until(backoffEnd).Round(time.Second)
			apiErr := apierrors.NewAPIError(
				fmt.Sprintf("Claude API rate limit exceeded (HTTP 429), retry after %v", remaining),
				fmt.Errorf("in backoff period until %v", backoffEnd),
			)
			apiErr.StatusCode = http.StatusTooManyRequests
			apiErr.RetryAfter = remaining
			return provider.QuotaResult{}, apiErr
		}
	}

	creds, fromKeychain, err := c.readCredentials(ctx)
	if err != nil {
		return provider.QuotaResult{}, err
	}
	refreshPath := c.credPath
	if fromKeychain {
		refreshPath = "" // CLI owns the Keychain update; no file mtime to poll.
	}

	// Refresh if expired.
	if creds.IsExpired() {
		slog.Debug("Claude token expired, attempting refresh")
		if refreshErr := RefreshToken(ctx, refreshPath); refreshErr != nil {
			return provider.QuotaResult{}, refreshErr
		}
		creds, _, err = c.readCredentials(ctx)
		if err != nil {
			return provider.QuotaResult{}, err
		}
	}

	apiClient := NewAPIClient(c.baseURL, c.httpClient)
	usage, err := apiClient.FetchUsage(ctx, creds.AccessToken)
	if err != nil {
		var domErr *apierrors.DomainError
		if errors.As(err, &domErr) {
			if domErr.Kind == "auth" {
				// On 401, try one refresh-and-retry cycle.
				slog.Debug("got 401, attempting token refresh and retry")
				if refreshErr := RefreshToken(ctx, refreshPath); refreshErr != nil {
					slog.Debug("token refresh failed", "error", refreshErr)
					return provider.QuotaResult{}, apierrors.NewAuthError("Claude authentication failed after refresh attempt", refreshErr)
				}
				creds, _, readErr := c.readCredentials(ctx)
				if readErr != nil {
					return provider.QuotaResult{}, readErr
				}
				usage, err = apiClient.FetchUsage(ctx, creds.AccessToken)
				if err != nil {
					var retryErr *apierrors.DomainError
					if !forced && errors.As(err, &retryErr) && retryErr.StatusCode == http.StatusTooManyRequests && retryErr.RetryAfter > 0 {
						if saveErr := saveBackoffState(c.backoffPath, time.Now().Add(retryErr.RetryAfter)); saveErr != nil {
							slog.Debug("failed to persist rate-limit backoff state", "error", saveErr)
						}
					}
					return provider.QuotaResult{}, err
				}
				// Retry succeeded
				if clearErr := clearBackoffState(c.backoffPath); clearErr != nil {
					slog.Debug("failed to clear rate-limit backoff state", "error", clearErr)
				}
				return convertUsage(creds, usage), nil
			} else if !forced && domErr.StatusCode == http.StatusTooManyRequests && domErr.RetryAfter > 0 {
				if saveErr := saveBackoffState(c.backoffPath, time.Now().Add(domErr.RetryAfter)); saveErr != nil {
					slog.Debug("failed to persist rate-limit backoff state", "error", saveErr)
				}
			}
		}
		return provider.QuotaResult{}, err
	}

	if clearErr := clearBackoffState(c.backoffPath); clearErr != nil {
		slog.Debug("failed to clear rate-limit backoff state", "error", clearErr)
	}
	return convertUsage(creds, usage), nil
}

// convertUsage transforms API types into a provider.QuotaResult.
func convertUsage(creds OAuthCredentials, usage *UsageResponse) provider.QuotaResult {
	result := provider.QuotaResult{
		Provider:  "claude",
		Status:    "ok",
		Plan:      creds.SubscriptionType,
		FetchedAt: time.Now(),
	}

	result.Windows = convertWindows(usage)

	if usage.ExtraUsage.IsEnabled {
		result.ExtraUsage = &provider.ExtraUsage{
			Enabled:     true,
			LimitUSD:    usage.ExtraUsage.MonthlyLimit,
			UsedUSD:     usage.ExtraUsage.UsedCredits,
			Utilization: usage.ExtraUsage.Utilization,
		}
	}

	return result
}

// convertWindows builds the window list: the session window, the all-models
// weekly window, then any model-scoped weekly windows.
//
// Weekly windows come from the limits array when the API provides it. The
// legacy seven_day* fields are only used as a fallback, and only when non-null,
// so a window the API no longer reports is not shown as a phantom 0% row.
func convertWindows(usage *UsageResponse) []provider.Window {
	// The session window is always listed; a null five_hour means no session
	// is in progress, which reads as 0% used.
	var fiveHour WindowData
	if usage.FiveHour != nil {
		fiveHour = *usage.FiveHour
	}
	windows := []provider.Window{newWindow("five_hour", fiveHour.Utilization, fiveHour.ResetsAt)}

	var weeklyAll *provider.Window
	var scoped []provider.Window
	for _, l := range usage.Limits {
		switch l.Kind {
		case limitKindWeeklyAll:
			if weeklyAll == nil {
				w := newWindow("seven_day", percentToUtilization(l.Percent), l.ResetsAt)
				weeklyAll = &w
			}
		case limitKindWeeklyScoped:
			slug := scopeSlug(l.Scope)
			if slug == "" {
				slog.Debug("skipping scoped weekly limit without a model name")
				continue
			}
			name := "seven_day_" + slug
			if slices.ContainsFunc(scoped, func(w provider.Window) bool { return w.Name == name }) {
				continue
			}
			scoped = append(scoped, newWindow(name, percentToUtilization(l.Percent), l.ResetsAt))
		default:
			slog.Debug("ignoring unrecognised usage limit", "kind", l.Kind)
		}
	}

	if weeklyAll == nil && usage.SevenDay != nil {
		w := newWindow("seven_day", usage.SevenDay.Utilization, usage.SevenDay.ResetsAt)
		weeklyAll = &w
	}
	if weeklyAll != nil {
		windows = append(windows, *weeklyAll)
	}
	if len(scoped) == 0 && usage.SevenDaySonnet != nil {
		scoped = append(scoped, newWindow("seven_day_sonnet", usage.SevenDaySonnet.Utilization, usage.SevenDaySonnet.ResetsAt))
	}
	return append(windows, scoped...)
}

// scopeSlug turns a limit scope's model name into a window-name suffix
// ("Fable" → "fable", "Opus 5" → "opus_5"). Returns "" when there is no model.
func scopeSlug(scope *LimitScope) string {
	if scope == nil || scope.Model == nil {
		return ""
	}
	name := scope.Model.DisplayName
	if strings.TrimSpace(name) == "" {
		name = scope.Model.ID
	}
	var sb strings.Builder
	pendingSep := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if pendingSep && sb.Len() > 0 {
				sb.WriteByte('_')
			}
			pendingSep = false
			sb.WriteRune(r)
			continue
		}
		pendingSep = true
	}
	return sb.String()
}

// percentToUtilization converts a 0–100 percentage to a clamped 0–1 fraction.
func percentToUtilization(percent float64) float64 {
	return min(max(percent/100, 0), 1)
}

// newWindow builds a provider.Window, parsing the ISO 8601 reset time.
func newWindow(name string, utilization float64, resetsAt string) provider.Window {
	w := provider.Window{
		Name:        name,
		Utilization: utilization,
	}
	if resetsAt != "" {
		t, err := time.Parse(time.RFC3339, resetsAt)
		if err != nil {
			slog.Debug("failed to parse resets_at", "window", name, "value", resetsAt, "error", err)
		} else {
			w.ResetsAt = t
		}
	}
	return w
}

// Compile-time interface check.
var _ provider.Provider = (*Claude)(nil)
var _ provider.BackoffResetter = (*Claude)(nil)
