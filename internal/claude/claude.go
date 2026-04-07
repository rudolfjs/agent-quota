package claude

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

// Claude implements provider.Provider for the Anthropic Claude API.
type Claude struct {
	credPath       string
	backoffPath    string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	baseURL        string
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
	return c
}

// Name returns "claude".
func (c *Claude) Name() string { return "claude" }

// Available reports whether Claude credentials are present and readable.
func (c *Claude) Available() bool {
	if c.defaultPathErr != nil {
		return false
	}
	_, err := ReadCredentials(c.credPath)
	return err == nil
}

// FetchQuota retrieves usage data from the Anthropic OAuth API.
func (c *Claude) FetchQuota(ctx context.Context) (provider.QuotaResult, error) {
	if c.defaultPathErr != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("cannot determine Claude configuration paths", c.defaultPathErr)
	}

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

	creds, err := ReadCredentials(c.credPath)
	if err != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("failed to read Claude credentials", err)
	}

	// Refresh if expired.
	if creds.IsExpired() {
		slog.Debug("Claude token expired, attempting refresh")
		if refreshErr := RefreshToken(ctx, c.credPath); refreshErr != nil {
			return provider.QuotaResult{}, refreshErr
		}
		creds, err = ReadCredentials(c.credPath)
		if err != nil {
			return provider.QuotaResult{}, apierrors.NewConfigError("failed to read credentials after refresh", err)
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
				if refreshErr := RefreshToken(ctx, c.credPath); refreshErr != nil {
					slog.Debug("token refresh failed", "error", refreshErr)
					return provider.QuotaResult{}, apierrors.NewAuthError("Claude authentication failed after refresh attempt", refreshErr)
				}
				creds, readErr := ReadCredentials(c.credPath)
				if readErr != nil {
					return provider.QuotaResult{}, apierrors.NewConfigError("failed to read credentials after refresh", readErr)
				}
				usage, err = apiClient.FetchUsage(ctx, creds.AccessToken)
				if err != nil {
					var retryErr *apierrors.DomainError
					if errors.As(err, &retryErr) && retryErr.StatusCode == http.StatusTooManyRequests && retryErr.RetryAfter > 0 {
						_ = saveBackoffState(c.backoffPath, time.Now().Add(retryErr.RetryAfter))
					}
					return provider.QuotaResult{}, err
				}
				// Retry succeeded
				clearBackoffState(c.backoffPath)
				return convertUsage(creds, usage), nil
			} else if domErr.StatusCode == http.StatusTooManyRequests && domErr.RetryAfter > 0 {
				_ = saveBackoffState(c.backoffPath, time.Now().Add(domErr.RetryAfter))
			}
		}
		return provider.QuotaResult{}, err
	}

	clearBackoffState(c.backoffPath)
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

	result.Windows = []provider.Window{
		parseWindow("five_hour", usage.FiveHour),
		parseWindow("seven_day", usage.SevenDay),
		parseWindow("seven_day_sonnet", usage.SevenDaySonnet),
	}

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

// parseWindow converts a WindowData to a provider.Window, parsing the ISO 8601 reset time.
func parseWindow(name string, wd WindowData) provider.Window {
	w := provider.Window{
		Name:        name,
		Utilization: wd.Utilization,
	}
	if wd.ResetsAt != "" {
		t, err := time.Parse(time.RFC3339, wd.ResetsAt)
		if err != nil {
			slog.Debug("failed to parse resets_at", "window", name, "value", wd.ResetsAt, "error", err)
		} else {
			w.ResetsAt = t
		}
	}
	return w
}

// Compile-time interface check.
var _ provider.Provider = (*Claude)(nil)
