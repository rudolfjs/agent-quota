package claude

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

// Claude implements provider.Provider for the Anthropic Claude API.
type Claude struct {
	credPath       string
	backoffPath    string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	baseURL        string
	forcedReset    bool             // set by ResetBackoff; suppresses backoff persistence on the next fetch
	source         credentialSource // file or keychain, wired in New()
	credPathSet    bool             // true when WithCredentialsPath was called explicitly
}

// Option configures a Claude provider instance.
type Option func(*Claude)

// WithCredentialsPath sets the path to the credentials JSON file and forces
// file-based credential reading (overrides darwin keychain selection).
func WithCredentialsPath(path string) Option {
	return func(c *Claude) {
		c.credPath = path
		c.credPathSet = true
	}
}

// WithCredentialSource injects a custom credentialSource. Used by tests to
// supply a fake; production callers should not use this directly.
func WithCredentialSource(src credentialSource) Option {
	return func(c *Claude) { c.source = src }
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
	// Wire credential source: use keychain on darwin unless an explicit file path
	// was given (explicit path overrides darwin default, for tests and debugging).
	if c.source == nil {
		if runtime.GOOS == "darwin" && !c.credPathSet {
			c.source = defaultKeychainSource()
		} else {
			c.source = fileSource{path: c.credPath}
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
	_, err := c.source.Read(context.Background())
	return err == nil
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

	creds, err := c.source.Read(ctx)
	if err != nil {
		return provider.QuotaResult{}, err
	}

	// Refresh if expired.
	if creds.IsExpired() {
		slog.Debug("Claude token expired, attempting refresh")
		if refreshErr := c.source.Refresh(ctx); refreshErr != nil {
			return provider.QuotaResult{}, refreshErr
		}
		creds, err = c.source.Read(ctx)
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
				if refreshErr := c.source.Refresh(ctx); refreshErr != nil {
					slog.Debug("token refresh failed", "error", refreshErr)
					return provider.QuotaResult{}, apierrors.NewAuthError("Claude authentication failed after refresh attempt", refreshErr)
				}
				creds, readErr := c.source.Read(ctx)
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
				return convertUsage(*creds, usage), nil
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
	return convertUsage(*creds, usage), nil
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
var _ provider.BackoffResetter = (*Claude)(nil)
