package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/fileutil"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

const (
	defaultUsageURL = "https://chatgpt.com/backend-api/wham/usage"
	defaultTokenURL = "https://auth.openai.com/oauth/token"
	defaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	// maxResponseBytes caps the response body we'll read from the OpenAI API
	// to prevent a malicious or misbehaving server from exhausting process memory.
	maxResponseBytes = 1 << 20 // 1 MiB
)

type authFile struct {
	AuthMode     string     `json:"auth_mode"`
	OpenAIAPIKey *string    `json:"OPENAI_API_KEY"`
	Tokens       authTokens `json:"tokens"`
	LastRefresh  string     `json:"last_refresh,omitempty"`
}

type authTokens struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	AccountID    string `json:"account_id,omitempty"`
}

type usageResponse struct {
	RateLimits          rateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]rateLimitSnapshot `json:"rateLimitsByLimitId"`

	PlanType             string                `json:"plan_type,omitempty"`
	RateLimit            *currentRateLimit     `json:"rate_limit,omitempty"`
	AdditionalRateLimits []additionalRateLimit `json:"additional_rate_limits,omitempty"`
	CodeReviewRateLimit  *currentRateLimit     `json:"code_review_rate_limit,omitempty"`
}

type rateLimitSnapshot struct {
	Credits   *creditsSnapshot `json:"credits,omitempty"`
	LimitID   string           `json:"limitId,omitempty"`
	LimitName string           `json:"limitName,omitempty"`
	PlanType  string           `json:"planType,omitempty"`
	Primary   *rateLimitWindow `json:"primary,omitempty"`
	Secondary *rateLimitWindow `json:"secondary,omitempty"`
}

type creditsSnapshot struct {
	Balance    string `json:"balance,omitempty"`
	HasCredits bool   `json:"hasCredits"`
	Unlimited  bool   `json:"unlimited"`
}

type rateLimitWindow struct {
	ResetsAt           int64 `json:"resetsAt,omitempty"`
	UsedPercent        int   `json:"usedPercent"`
	WindowDurationMins int64 `json:"windowDurationMins,omitempty"`
}

type currentRateLimit struct {
	PrimaryWindow   *currentWindow `json:"primary_window,omitempty"`
	SecondaryWindow *currentWindow `json:"secondary_window,omitempty"`
}

type additionalRateLimit struct {
	LimitName      string           `json:"limit_name,omitempty"`
	MeteredFeature string           `json:"metered_feature,omitempty"`
	RateLimit      currentRateLimit `json:"rate_limit"`
}

type currentWindow struct {
	ResetAt            int64   `json:"reset_at,omitempty"`
	UsedPercent        float64 `json:"used_percent"`
	LimitWindowSeconds int64   `json:"limit_window_seconds,omitempty"`
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

// OpenAI implements provider.Provider for ChatGPT/Codex OAuth quota data.
type OpenAI struct {
	authPath       string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	usageURL       string
	tokenURL       string
}

// Option configures an OpenAI provider instance.
type Option func(*OpenAI)

func WithAuthPath(path string) Option {
	return func(o *OpenAI) { o.authPath = path }
}

func WithHTTPClient(client *http.Client) Option {
	return func(o *OpenAI) { o.httpClient = client }
}

func WithUsageURL(url string) Option {
	return func(o *OpenAI) { o.usageURL = url }
}

func WithTokenURL(url string) Option {
	return func(o *OpenAI) { o.tokenURL = url }
}

// defaultHTTPClient is the HTTP client used when none is provided via WithHTTPClient.
// A 30-second timeout prevents hung API servers from blocking the process indefinitely.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

func New(opts ...Option) *OpenAI {
	o := &OpenAI{
		httpClient: defaultHTTPClient,
		usageURL:   defaultUsageURL,
		tokenURL:   defaultTokenURL,
	}
	for _, opt := range opts {
		opt(o)
	}
	// Set default auth path only if not overridden by WithAuthPath.
	if o.authPath == "" {
		path, err := DefaultAuthPath()
		o.authPath = path
		o.defaultPathErr = err
	}
	return o
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) Available() bool {
	if o.defaultPathErr != nil {
		return false
	}
	auth, err := readAuthFile(o.authPath)
	if err != nil {
		return false
	}
	return auth.Tokens.AccessToken != "" && auth.Tokens.RefreshToken != ""
}

func (o *OpenAI) FetchQuota(ctx context.Context) (provider.QuotaResult, error) {
	if o.defaultPathErr != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("cannot determine OpenAI auth path", o.defaultPathErr)
	}
	auth, err := readAuthFile(o.authPath)
	if err != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("failed to read OpenAI auth", err)
	}
	if auth.Tokens.AccessToken == "" {
		return provider.QuotaResult{}, apierrors.NewAuthError("OpenAI authentication is not configured", fmt.Errorf("missing access token"))
	}

	usage, err := o.fetchUsage(ctx, auth.Tokens.AccessToken)
	if err != nil {
		var domErr *apierrors.DomainError
		if !errorsAs(err, &domErr) || domErr.Kind != "auth" {
			return provider.QuotaResult{}, err
		}

		if auth.Tokens.RefreshToken == "" {
			return provider.QuotaResult{}, apierrors.NewAuthError("OpenAI authentication expired; run `codex login` again", err)
		}

		slog.Debug("OpenAI usage request returned auth error, attempting refresh")
		refreshed, refreshErr := o.refreshAccessToken(ctx, auth.Tokens.RefreshToken)
		if refreshErr != nil {
			return provider.QuotaResult{}, refreshErr
		}

		auth.Tokens.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			auth.Tokens.RefreshToken = refreshed.RefreshToken
		}
		if refreshed.IDToken != "" {
			auth.Tokens.IDToken = refreshed.IDToken
		}
		auth.LastRefresh = time.Now().UTC().Format(time.RFC3339)
		if writeErr := writeAuthFile(o.authPath, auth); writeErr != nil {
			return provider.QuotaResult{}, apierrors.NewConfigError("failed to persist refreshed OpenAI auth", writeErr)
		}

		usage, err = o.fetchUsage(ctx, auth.Tokens.AccessToken)
		if err != nil {
			return provider.QuotaResult{}, err
		}
	}

	return convertUsage(usage), nil
}

func (o *OpenAI) fetchUsage(ctx context.Context, accessToken string) (*usageResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.usageURL, nil)
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to create OpenAI usage request", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-quota")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("OpenAI usage request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, apierrors.NewAuthError("OpenAI authentication expired; attempting refresh", fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := apierrors.NewAPIError(fmt.Sprintf("OpenAI usage API returned an unexpected status (HTTP %d)", resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode))
		apiErr.StatusCode = resp.StatusCode
		return nil, apiErr
	}

	var usage usageResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&usage); err != nil {
		return nil, apierrors.NewAPIError("failed to parse OpenAI usage response", err)
	}
	return &usage, nil
}

func (o *OpenAI) refreshAccessToken(ctx context.Context, refreshToken string) (*refreshResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", defaultClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.tokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to create OpenAI token refresh request", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-quota")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("OpenAI token refresh failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, apierrors.NewAuthError("OpenAI authentication expired; run `codex login` again", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var refreshed refreshResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&refreshed); err != nil {
		return nil, apierrors.NewAPIError("failed to parse OpenAI token refresh response", err)
	}
	if refreshed.AccessToken == "" {
		return nil, apierrors.NewAuthError("OpenAI authentication expired; run `codex login` again", fmt.Errorf("missing access_token in refresh response"))
	}
	return &refreshed, nil
}

func convertUsage(usage *usageResponse) provider.QuotaResult {
	result := provider.QuotaResult{
		Provider:  "openai",
		Status:    "ok",
		FetchedAt: time.Now(),
	}

	if usage.PlanType != "" {
		result.Plan = usage.PlanType
	}
	if usage.RateLimit != nil {
		result.Windows = append(result.Windows, currentRateLimitWindows(*usage.RateLimit, "")...)
	}
	if len(usage.AdditionalRateLimits) > 0 {
		limits := append([]additionalRateLimit(nil), usage.AdditionalRateLimits...)
		sort.Slice(limits, func(i, j int) bool {
			return currentRateLimitPrefix(limits[i]) < currentRateLimitPrefix(limits[j])
		})
		for _, limit := range limits {
			result.Windows = append(result.Windows, currentRateLimitWindows(limit.RateLimit, currentRateLimitPrefix(limit))...)
		}
	}
	if usage.CodeReviewRateLimit != nil {
		result.Windows = append(result.Windows, currentRateLimitWindows(*usage.CodeReviewRateLimit, "code_review")...)
	}
	if result.Plan != "" || len(result.Windows) > 0 {
		return result
	}

	if len(usage.RateLimitsByLimitID) > 0 {
		keys := make([]string, 0, len(usage.RateLimitsByLimitID))
		for key := range usage.RateLimitsByLimitID {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			snapshot := usage.RateLimitsByLimitID[key]
			if result.Plan == "" && snapshot.PlanType != "" {
				result.Plan = snapshot.PlanType
			}
			prefix := ""
			if len(keys) > 1 {
				prefix = key
			}
			result.Windows = append(result.Windows, snapshotWindows(snapshot, prefix)...)
		}
	}

	if result.Plan == "" {
		result.Plan = usage.RateLimits.PlanType
	}
	if len(result.Windows) == 0 {
		result.Windows = snapshotWindows(usage.RateLimits, "")
	}

	return result
}

func snapshotWindows(snapshot rateLimitSnapshot, prefix string) []provider.Window {
	windows := make([]provider.Window, 0, 2)
	if snapshot.Primary != nil {
		windows = append(windows, toWindow(*snapshot.Primary, prefix))
	}
	if snapshot.Secondary != nil {
		windows = append(windows, toWindow(*snapshot.Secondary, prefix))
	}
	return windows
}

func currentRateLimitWindows(limit currentRateLimit, prefix string) []provider.Window {
	windows := make([]provider.Window, 0, 2)
	if limit.PrimaryWindow != nil {
		windows = append(windows, toCurrentWindow(*limit.PrimaryWindow, prefix))
	}
	if limit.SecondaryWindow != nil {
		windows = append(windows, toCurrentWindow(*limit.SecondaryWindow, prefix))
	}
	return windows
}

func toCurrentWindow(window currentWindow, prefix string) provider.Window {
	name := currentWindowName(window.LimitWindowSeconds)
	if prefix != "" {
		name = prefix + "_" + name
	}
	return provider.Window{
		Name:        name,
		Utilization: clampUtilization(window.UsedPercent / 100.0),
		ResetsAt:    parseEpoch(window.ResetAt),
	}
}

func currentRateLimitPrefix(limit additionalRateLimit) string {
	if limit.MeteredFeature != "" {
		return normalizePrefix(limit.MeteredFeature)
	}
	return normalizePrefix(limit.LimitName)
}

func toWindow(window rateLimitWindow, prefix string) provider.Window {
	name := windowName(window.WindowDurationMins)
	if prefix != "" {
		name = prefix + "_" + name
	}
	return provider.Window{
		Name:        name,
		Utilization: clampUtilization(float64(window.UsedPercent) / 100.0),
		ResetsAt:    parseEpoch(window.ResetsAt),
	}
}

func windowName(durationMins int64) string {
	switch durationMins {
	case 300:
		return "five_hour"
	case 7 * 24 * 60:
		return "seven_day"
	case 0:
		return "window"
	default:
		return fmt.Sprintf("%d_min", durationMins)
	}
}

func currentWindowName(durationSeconds int64) string {
	switch durationSeconds {
	case 5 * 60 * 60:
		return "five_hour"
	case 7 * 24 * 60 * 60:
		return "seven_day"
	case 0:
		return "window"
	default:
		return fmt.Sprintf("%d_sec", durationSeconds)
	}
}

func parseEpoch(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	if value >= 1_000_000_000_000 {
		return time.UnixMilli(value).UTC()
	}
	return time.Unix(value, 0).UTC()
}

func clampUtilization(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func normalizePrefix(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "limit"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}

	result := strings.Trim(b.String(), "_")
	if result == "" {
		return "limit"
	}
	if result == "codex_bengalfox" {
		return "codex_spark"
	}
	return result
}

func readAuthFile(path string) (authFile, error) {
	fileutil.WarnInsecurePermissions(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return authFile{}, fmt.Errorf("read auth file: %w", err)
	}
	var auth authFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return authFile{}, fmt.Errorf("parse auth file: %w", err)
	}
	return auth, nil
}

func writeAuthFile(path string, auth authFile) error {
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal auth file: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write auth file: %w", err)
	}
	return nil
}

// DefaultAuthPath returns the default path to the OpenAI/Codex auth file.
// Returns an error if the user home directory cannot be determined.
func DefaultAuthPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for OpenAI auth: %w", err)
	}
	return home + "/.codex/auth.json", nil
}

var _ provider.Provider = (*OpenAI)(nil)

func errorsAs(err error, target **apierrors.DomainError) bool {
	var domErr *apierrors.DomainError
	if err == nil {
		return false
	}
	if ok := errors.As(err, &domErr); ok {
		*target = domErr
		return true
	}
	return false
}
