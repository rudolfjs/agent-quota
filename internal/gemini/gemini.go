package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/fileutil"
	"github.com/rudolfjs/agent-quota/internal/provider"
)

const (
	defaultBaseURL = "https://cloudcode-pa.googleapis.com"

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

// Gemini implements provider.Provider for Gemini Code Assist OAuth quota data.
type Gemini struct {
	credPath       string
	defaultPathErr error // non-nil when home dir lookup failed and no explicit path was given
	httpClient     *http.Client
	baseURL        string
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

// defaultHTTPClient is the HTTP client used when none is provided via WithHTTPClient.
// A 30-second timeout prevents hung API servers from blocking the process indefinitely.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

func New(opts ...Option) *Gemini {
	g := &Gemini{
		httpClient: defaultHTTPClient,
		baseURL:    defaultBaseURL,
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
		slog.Debug("Gemini token expired, attempting refresh")
		if refreshErr := RefreshToken(ctx, g.credPath); refreshErr != nil {
			return provider.QuotaResult{}, refreshErr
		}
		creds, err = readCredentials(g.credPath)
		if err != nil {
			return provider.QuotaResult{}, apierrors.NewConfigError("failed to read credentials after refresh", err)
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
		apiErr := apierrors.NewAPIError(fmt.Sprintf("Gemini API returned an unexpected status (HTTP %d)", resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode))
		apiErr.StatusCode = resp.StatusCode
		return nil, apiErr
	}

	var out T
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&out); err != nil {
		return nil, apierrors.NewAPIError("failed to parse Gemini response", err)
	}
	return &out, nil
}

func convertQuota(loadResp *loadCodeAssistResponse, quotaResp *retrieveUserQuotaResponse) provider.QuotaResult {
	result := provider.QuotaResult{
		Provider:  "gemini",
		Status:    "ok",
		Plan:      planFromLoad(loadResp),
		FetchedAt: time.Now(),
	}

	buckets := append([]quotaBucket(nil), quotaResp.Buckets...)
	sort.Slice(buckets, func(i, j int) bool {
		iMaj, iMin := geminiModelVersion(buckets[i].ModelID)
		jMaj, jMin := geminiModelVersion(buckets[j].ModelID)
		if iMaj != jMaj {
			return iMaj > jMaj // higher major version first (3.x before 2.x)
		}
		if iMin != jMin {
			return iMin > jMin // higher minor version first
		}
		return geminiModelTypeRank(buckets[i].ModelID) < geminiModelTypeRank(buckets[j].ModelID)
	})
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

// geminiModelVersion parses the major and minor version from a model ID like
// "gemini-2.5-flash" → (2, 5). Returns (0, 0) for unrecognised IDs.
func geminiModelVersion(modelID string) (major, minor int) {
	if !strings.HasPrefix(modelID, "gemini-") {
		return 0, 0
	}
	rest := strings.TrimPrefix(modelID, "gemini-")
	// rest = "2.5-flash" — first segment before the next hyphen is the version.
	hyphenIdx := strings.Index(rest, "-")
	verStr := rest
	if hyphenIdx >= 0 {
		verStr = rest[:hyphenIdx]
	}
	parts := strings.SplitN(verStr, ".", 2)
	major, _ = strconv.Atoi(parts[0])
	if len(parts) == 2 {
		minor, _ = strconv.Atoi(parts[1])
	}
	return major, minor
}

// geminiModelTypeRank returns a sort rank for the model variant so that
// Pro < Flash < Flash Lite within the same version group.
func geminiModelTypeRank(modelID string) int {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "flash-lite") || strings.Contains(lower, "flash-8b"):
		return 2
	case strings.Contains(lower, "flash"):
		return 1
	case strings.Contains(lower, "pro"):
		return 0
	default:
		return 3
	}
}

var _ provider.Provider = (*Gemini)(nil)
