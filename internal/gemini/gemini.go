package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"time"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
	"github.com/schnetlerr/agent-quota/internal/fileutil"
	"github.com/schnetlerr/agent-quota/internal/provider"
)

const (
	defaultBaseURL  = "https://cloudcode-pa.googleapis.com"
	defaultTokenURL = "https://oauth2.googleapis.com/token"

	// googleClientID and googleSecret are the OAuth 2.0 credentials for the
	// Gemini CLI's native/installed-app registration. Per RFC 8252 §8.4 and
	// Google's own documentation for installed apps, the client secret is NOT
	// confidential — it is embedded in the distributed client binary by design.
	// Any party with access to the binary can extract it via `strings`, and
	// Google's token endpoint cannot use it to prove client identity. It exists
	// only to satisfy the OAuth form parameter requirement.
	// See: https://developers.google.com/identity/protocols/oauth2/native-app
	googleClientID = "REDACTED-GOOGLE-CLIENT-ID"
	googleSecret   = "REDACTED-GOOGLE-CLIENT-SECRET"

	// maxResponseBytes caps the response body we'll read from any Gemini API
	// endpoint to prevent a malicious or misbehaving server from exhausting
	// process memory.
	maxResponseBytes = 1 << 20 // 1 MiB
)

type oauthCredentials struct {
	AccessToken  string `json:"access_token"`
	Scope        string `json:"scope,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	IDToken      string `json:"id_token,omitempty"`
	ExpiryDate   int64  `json:"expiry_date,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type loadCodeAssistResponse struct {
	CurrentTier             *tier  `json:"currentTier,omitempty"`
	CloudAICompanionProject string `json:"cloudaicompanionProject,omitempty"`
	PaidTier                *tier  `json:"paidTier,omitempty"`
}

type tier struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type retrieveUserQuotaResponse struct {
	Buckets []quotaBucket `json:"buckets"`
}

type quotaBucket struct {
	RemainingAmount   string  `json:"remainingAmount,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	TokenType         string  `json:"tokenType,omitempty"`
	ModelID           string  `json:"modelId,omitempty"`
}

type tokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
	TokenType   string `json:"token_type,omitempty"`
	IDToken     string `json:"id_token,omitempty"`
}

// Gemini implements provider.Provider for Gemini Code Assist OAuth quota data.
type Gemini struct {
	credPath       string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	baseURL        string
	tokenURL       string
}

// Option configures a Gemini provider instance.
type Option func(*Gemini)

func WithCredentialsPath(path string) Option {
	return func(g *Gemini) { g.credPath = path }
}

func WithHTTPClient(client *http.Client) Option {
	return func(g *Gemini) { g.httpClient = client }
}

func WithBaseURL(url string) Option {
	return func(g *Gemini) { g.baseURL = url }
}

func WithTokenURL(url string) Option {
	return func(g *Gemini) { g.tokenURL = url }
}

// defaultHTTPClient is the HTTP client used when none is provided via WithHTTPClient.
// A 30-second timeout prevents hung API servers from blocking the process indefinitely.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

func New(opts ...Option) *Gemini {
	g := &Gemini{
		httpClient: defaultHTTPClient,
		baseURL:    defaultBaseURL,
		tokenURL:   defaultTokenURL,
	}
	for _, opt := range opts {
		opt(g)
	}
	// Set default credentials path only if not overridden by WithCredentialsPath.
	if g.credPath == "" {
		path, err := DefaultCredentialsPath()
		g.credPath = path
		g.defaultPathErr = err
	}
	return g
}

func (g *Gemini) Name() string { return "gemini" }

func (g *Gemini) Available() bool {
	if g.defaultPathErr != nil {
		return false
	}
	creds, err := readCredentials(g.credPath)
	if err != nil {
		return false
	}
	return creds.AccessToken != "" && creds.RefreshToken != ""
}

func (g *Gemini) FetchQuota(ctx context.Context) (provider.QuotaResult, error) {
	if g.defaultPathErr != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("cannot determine Gemini credentials path", g.defaultPathErr)
	}
	creds, err := readCredentials(g.credPath)
	if err != nil {
		return provider.QuotaResult{}, apierrors.NewConfigError("failed to read Gemini OAuth credentials", err)
	}

	if creds.IsExpired() {
		creds, err = g.refreshCredentials(ctx, creds)
		if err != nil {
			return provider.QuotaResult{}, err
		}
	}

	loadResp, err := g.loadCodeAssist(ctx, creds.AccessToken)
	if err != nil {
		return provider.QuotaResult{}, err
	}
	if loadResp.CloudAICompanionProject == "" {
		return provider.QuotaResult{}, apierrors.NewAPIError("Gemini project is not available for this account", fmt.Errorf("empty cloudaicompanionProject"))
	}

	quotaResp, err := g.retrieveUserQuota(ctx, creds.AccessToken, loadResp.CloudAICompanionProject)
	if err != nil {
		return provider.QuotaResult{}, err
	}

	return convertQuota(loadResp, quotaResp), nil
}

func (g *Gemini) loadCodeAssist(ctx context.Context, accessToken string) (*loadCodeAssistResponse, error) {
	body := map[string]any{
		"metadata": map[string]any{
			"ideType":    "IDE_UNSPECIFIED",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	}
	return doJSONRequest[loadCodeAssistResponse](ctx, g.httpClient, http.MethodPost, g.baseURL+"/v1internal:loadCodeAssist", accessToken, body)
}

func (g *Gemini) retrieveUserQuota(ctx context.Context, accessToken, project string) (*retrieveUserQuotaResponse, error) {
	body := map[string]any{"project": project}
	return doJSONRequest[retrieveUserQuotaResponse](ctx, g.httpClient, http.MethodPost, g.baseURL+"/v1internal:retrieveUserQuota", accessToken, body)
}

func doJSONRequest[T any](ctx context.Context, client *http.Client, method, targetURL, accessToken string, body any) (*T, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, apierrors.NewAPIError("failed to serialize Gemini request", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, bytes.NewReader(data))
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to create Gemini request", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-quota")

	resp, err := client.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("Gemini request failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, apierrors.NewAuthError("Gemini authentication expired; refresh required", fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apierrors.NewAPIError("Gemini API returned an unexpected status", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var out T
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, apierrors.NewAPIError("failed to parse Gemini response", err)
	}
	return &out, nil
}

func (g *Gemini) refreshCredentials(ctx context.Context, creds oauthCredentials) (oauthCredentials, error) {
	if creds.RefreshToken == "" {
		return oauthCredentials{}, apierrors.NewAuthError("Gemini authentication expired; run `gemini` again", fmt.Errorf("missing refresh token"))
	}

	form := url.Values{}
	form.Set("client_id", googleClientID)
	form.Set("client_secret", googleSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", creds.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return oauthCredentials{}, apierrors.NewNetworkError("failed to create Gemini token refresh request", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agent-quota")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return oauthCredentials{}, apierrors.NewNetworkError("Gemini token refresh failed", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return oauthCredentials{}, apierrors.NewAuthError("Gemini authentication expired; run `gemini` again", fmt.Errorf("HTTP %d", resp.StatusCode))
	}

	var refreshed tokenRefreshResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&refreshed); err != nil {
		return oauthCredentials{}, apierrors.NewAPIError("failed to parse Gemini token refresh response", err)
	}
	if refreshed.AccessToken == "" {
		return oauthCredentials{}, apierrors.NewAuthError("Gemini authentication expired; run `gemini` again", fmt.Errorf("missing access_token in refresh response"))
	}

	creds.AccessToken = refreshed.AccessToken
	creds.Scope = refreshed.Scope
	creds.TokenType = refreshed.TokenType
	creds.IDToken = refreshed.IDToken
	if refreshed.ExpiresIn > 0 {
		creds.ExpiryDate = time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second).UnixMilli()
	}
	if err := writeCredentials(g.credPath, creds); err != nil {
		return oauthCredentials{}, apierrors.NewConfigError("failed to persist refreshed Gemini credentials", err)
	}
	return creds, nil
}

func convertQuota(loadResp *loadCodeAssistResponse, quotaResp *retrieveUserQuotaResponse) provider.QuotaResult {
	result := provider.QuotaResult{
		Provider:  "gemini",
		Status:    "ok",
		Plan:      planFromLoad(loadResp),
		FetchedAt: time.Now(),
	}

	buckets := append([]quotaBucket(nil), quotaResp.Buckets...)
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].ModelID < buckets[j].ModelID })
	for _, bucket := range buckets {
		result.Windows = append(result.Windows, provider.Window{
			Name:        bucket.ModelID,
			Utilization: clampUtilization(1 - bucket.RemainingFraction),
			ResetsAt:    parseResetTime(bucket.ResetTime),
		})
	}

	return result
}

func planFromLoad(loadResp *loadCodeAssistResponse) string {
	if loadResp.PaidTier != nil && loadResp.PaidTier.ID != "" {
		return loadResp.PaidTier.ID
	}
	if loadResp.CurrentTier != nil && loadResp.CurrentTier.ID != "" {
		return loadResp.CurrentTier.ID
	}
	return ""
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

func readCredentials(path string) (oauthCredentials, error) {
	fileutil.WarnInsecurePermissions(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("read credentials file: %w", err)
	}
	var creds oauthCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return oauthCredentials{}, fmt.Errorf("parse credentials file: %w", err)
	}
	return creds, nil
}

func writeCredentials(path string, creds oauthCredentials) error {
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials file: %w", err)
	}
	if err := fileutil.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials file: %w", err)
	}
	return nil
}

// DefaultCredentialsPath returns the default path to the Gemini credentials file.
// Returns an error if the user home directory cannot be determined.
func DefaultCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory for Gemini credentials: %w", err)
	}
	return home + "/.gemini/oauth_creds.json", nil
}

func (c oauthCredentials) IsExpired() bool {
	if c.ExpiryDate == 0 {
		return true
	}
	expiry := time.UnixMilli(c.ExpiryDate)
	return time.Now().After(expiry.Add(-60 * time.Second))
}

var _ provider.Provider = (*Gemini)(nil)
