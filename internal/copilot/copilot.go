package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/fileutil"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

const (
	defaultBaseURL = "https://api.github.com"
	userPath       = "/copilot_internal/user"

	// maxResponseBytes caps the response body we'll read from the Copilot API
	// to prevent a malicious or misbehaving server from exhausting process memory.
	maxResponseBytes = 1 << 20 // 1 MiB
)

var errTokenNotConfigured = errors.New("copilot token not configured")

type configFile struct {
	LastLoggedInUser *loggedInUser     `json:"last_logged_in_user,omitempty"`
	LoggedInUsers    []loggedInUser    `json:"logged_in_users,omitempty"`
	CopilotTokens    map[string]string `json:"copilot_tokens,omitempty"`
}

type loggedInUser struct {
	Host  string `json:"host"`
	Login string `json:"login"`
}

type userResponse struct {
	CopilotPlan       string         `json:"copilot_plan,omitempty"`
	QuotaResetDateUTC string         `json:"quota_reset_date_utc,omitempty"`
	QuotaResetDate    string         `json:"quota_reset_date,omitempty"`
	QuotaSnapshots    quotaSnapshots `json:"quota_snapshots,omitempty"`
}

type quotaSnapshots struct {
	Chat                *quotaSnapshot `json:"chat,omitempty"`
	Completions         *quotaSnapshot `json:"completions,omitempty"`
	PremiumInteractions *quotaSnapshot `json:"premium_interactions,omitempty"`
}

type quotaSnapshot struct {
	Entitlement      float64 `json:"entitlement,omitempty"`
	PercentRemaining float64 `json:"percent_remaining,omitempty"`
	Unlimited        bool    `json:"unlimited,omitempty"`
}

// Copilot implements provider.Provider for GitHub Copilot CLI quota data.
type Copilot struct {
	configPath     string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	baseURL        string
}

// Option configures a Copilot provider instance.
type Option func(*Copilot)

func WithConfigPath(path string) Option {
	return func(c *Copilot) { c.configPath = path }
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Copilot) { c.httpClient = client }
}

func WithBaseURL(url string) Option {
	return func(c *Copilot) { c.baseURL = strings.TrimRight(url, "/") }
}

// defaultHTTPClient is the HTTP client used when none is provided via WithHTTPClient.
// A 30-second timeout prevents hung API servers from blocking the process indefinitely.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

func New(opts ...Option) *Copilot {
	c := &Copilot{
		httpClient: defaultHTTPClient,
		baseURL:    defaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.configPath == "" {
		path, err := DefaultConfigPath()
		c.configPath = path
		c.defaultPathErr = err
	}
	return c
}

func (c *Copilot) Name() string { return "copilot" }

func (c *Copilot) Available() bool {
	_, _, err := c.resolveToken()
	return err == nil
}

func (c *Copilot) FetchQuota(ctx context.Context) (provider.QuotaResult, error) {
	token, host, err := c.resolveToken()
	if err != nil {
		switch {
		case errors.Is(err, errTokenNotConfigured):
			slog.Debug("copilot token not configured", "error", err)
			return provider.QuotaResult{}, apierrors.NewAuthError("Copilot authentication is not configured", err)
		case c.defaultPathErr != nil && c.configPath == "":
			slog.Debug("copilot config path unavailable", "error", c.defaultPathErr)
			return provider.QuotaResult{}, apierrors.NewConfigError("cannot determine Copilot config path", c.defaultPathErr)
		default:
			slog.Debug("failed to resolve copilot token", "error", err)
			return provider.QuotaResult{}, apierrors.NewConfigError("failed to read Copilot config", err)
		}
	}

	baseURL := c.baseURL
	if baseURL == defaultBaseURL && host != "" {
		baseURL = apiBaseURL(host)
	}

	usage, err := c.fetchUser(ctx, baseURL, token)
	if err != nil {
		return provider.QuotaResult{}, err
	}

	return convertUsage(usage), nil
}

func (c *Copilot) fetchUser(ctx context.Context, baseURL, token string) (*userResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+userPath, nil)
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to create Copilot usage request", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-quota")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("Copilot usage request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, apierrors.NewAuthError("Copilot authentication failed; run `copilot login` again", fmt.Errorf("HTTP %d", resp.StatusCode))
	case http.StatusForbidden:
		return nil, apierrors.NewAuthError("Copilot access is denied; check your Copilot plan or organization policy", fmt.Errorf("HTTP %d", resp.StatusCode))
	case http.StatusPaymentRequired:
		return nil, apierrors.NewAPIError("Copilot premium requests require billing setup or a billing selection", fmt.Errorf("HTTP %d", resp.StatusCode))
	default:
		slog.Debug("copilot API unexpected status", slog.Int("status_code", resp.StatusCode))
		apiErr := apierrors.NewAPIError(fmt.Sprintf("Copilot API returned an unexpected status (HTTP %d)", resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode))
		apiErr.StatusCode = resp.StatusCode
		return nil, apiErr
	}

	var usage userResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&usage); err != nil {
		slog.Debug("failed to decode copilot response", "error", err)
		return nil, apierrors.NewAPIError("failed to parse Copilot usage response", err)
	}
	return &usage, nil
}

func (c *Copilot) resolveToken() (token, host string, err error) {
	for _, name := range []string{"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value, "", nil
		}
	}

	if c.configPath == "" {
		if c.defaultPathErr != nil {
			return "", "", c.defaultPathErr
		}
		return "", "", fmt.Errorf("empty Copilot config path")
	}

	cfg, err := readConfig(c.configPath)
	if err != nil {
		return "", "", err
	}
	if token, host, ok := cfg.selectedToken(); ok {
		return token, host, nil
	}
	return "", "", errTokenNotConfigured
}

func readConfig(path string) (configFile, error) {
	fileutil.WarnInsecurePermissions(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return configFile{}, apierrors.NewConfigError("failed to read Copilot config file", err)
	}
	var cfg configFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return configFile{}, apierrors.NewConfigError("failed to parse Copilot config file", err)
	}
	return cfg, nil
}

func (f configFile) selectedToken() (token, host string, ok bool) {
	if f.LastLoggedInUser != nil {
		if token, ok := f.CopilotTokens[f.LastLoggedInUser.Host+":"+f.LastLoggedInUser.Login]; ok && strings.TrimSpace(token) != "" {
			return token, f.LastLoggedInUser.Host, true
		}
	}

	for _, user := range f.LoggedInUsers {
		if token, ok := f.CopilotTokens[user.Host+":"+user.Login]; ok && strings.TrimSpace(token) != "" {
			return token, user.Host, true
		}
	}

	keys := make([]string, 0, len(f.CopilotTokens))
	for k := range f.CopilotTokens {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, key := range keys {
		token := f.CopilotTokens[key]
		if strings.TrimSpace(token) == "" {
			continue
		}
		if host, ok := parseTokenKeyHost(key); ok {
			return token, host, true
		}
		return token, "", true
	}

	return "", "", false
}

func parseTokenKeyHost(key string) (string, bool) {
	idx := strings.LastIndex(key, ":")
	if idx <= 0 {
		return "", false
	}
	return key[:idx], true
}

func apiBaseURL(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return defaultBaseURL
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return defaultBaseURL
	}
	if !strings.HasPrefix(parsed.Host, "api.") {
		parsed.Host = "api." + parsed.Host
	}
	parsed.Scheme = "https" // enforce HTTPS to prevent token leakage over plain HTTP
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func convertUsage(usage *userResponse) provider.QuotaResult {
	resetAt := parseResetTime(usage.QuotaResetDateUTC)
	if resetAt.IsZero() {
		resetAt = parseResetTime(usage.QuotaResetDate)
	}

	result := provider.QuotaResult{
		Provider:  "copilot",
		Status:    "ok",
		Plan:      usage.CopilotPlan,
		FetchedAt: time.Now(),
	}

	appendWindow := func(name string, snapshot *quotaSnapshot) {
		if snapshot == nil {
			return
		}
		result.Windows = append(result.Windows, provider.Window{
			Name:        name,
			Utilization: snapshotUtilization(*snapshot),
			ResetsAt:    resetAt,
		})
	}

	appendWindow("premium_interactions", usage.QuotaSnapshots.PremiumInteractions)
	appendWindow("chat", usage.QuotaSnapshots.Chat)
	appendWindow("completions", usage.QuotaSnapshots.Completions)

	return result
}

func snapshotUtilization(snapshot quotaSnapshot) float64 {
	if snapshot.Unlimited {
		return 0
	}
	return clampUtilization(1 - snapshot.PercentRemaining/100.0)
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

func parseResetTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// DefaultConfigPath returns the default path to the Copilot config file.
// Returns an error if the user home directory cannot be determined.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for Copilot config: %w", err)
	}
	return home + "/.copilot/config.json", nil
}

var _ provider.Provider = (*Copilot)(nil)
