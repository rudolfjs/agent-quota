// Package selfupdate implements the `aq self-update` command: resolving
// the latest GitHub release, downloading the platform archive, verifying
// the sha256 checksum, and atomically swapping the running binary.
//
// The package is network- and filesystem-heavy but free of CLI concerns —
// cobra wiring lives in internal/cli/selfupdate.go.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	apierrors "github.com/schnetlerr/agent-quota/internal/errors"
)

// maxReleaseResponseBytes caps the GitHub API response size. Real responses
// are a few KB; anything larger is a malicious/misbehaving server.
const maxReleaseResponseBytes = 1 << 20 // 1 MiB

// Release mirrors the subset of the GitHub "release" JSON we consume.
type Release struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// ReleaseClient talks to a GitHub-compatible releases API. Tests inject
// an httptest server URL via baseURL.
type ReleaseClient struct {
	baseURL    string // e.g. "https://api.github.com"
	ownerRepo  string // e.g. "rudolfjs/agent-quota"
	httpClient *http.Client
}

// NewReleaseClient returns a client configured for the given owner/repo.
// Pass an empty baseURL for the real GitHub API.
func NewReleaseClient(baseURL, ownerRepo string, httpClient *http.Client) *ReleaseClient {
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ReleaseClient{baseURL: baseURL, ownerRepo: ownerRepo, httpClient: httpClient}
}

// Latest fetches the "latest" (non-prerelease, non-draft) release.
// GitHub's /releases/latest endpoint already filters these out, so prerelease
// users should call List and pick their own.
func (c *ReleaseClient) Latest(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.baseURL, c.ownerRepo)
	return c.fetchOne(ctx, url)
}

// List returns all releases (including prereleases). Useful when the user
// opts into prereleases via --pre.
func (c *ReleaseClient) List(ctx context.Context) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=30", c.baseURL, c.ownerRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to build releases request", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("releases request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		apiErr := apierrors.NewAPIError(fmt.Sprintf("GitHub API returned HTTP %d while listing releases", resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode))
		apiErr.StatusCode = resp.StatusCode
		return nil, apiErr
	}

	var releases []Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseResponseBytes)).Decode(&releases); err != nil {
		return nil, apierrors.NewAPIError("failed to parse releases response", err)
	}
	return releases, nil
}

func (c *ReleaseClient) fetchOne(ctx context.Context, url string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, apierrors.NewNetworkError("failed to build release request", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, apierrors.NewNetworkError("release request failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, apierrors.NewAPIError("no releases found for "+c.ownerRepo, fmt.Errorf("HTTP 404"))
	}
	if resp.StatusCode != http.StatusOK {
		apiErr := apierrors.NewAPIError(fmt.Sprintf("GitHub API returned HTTP %d", resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode))
		apiErr.StatusCode = resp.StatusCode
		return nil, apiErr
	}

	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseResponseBytes)).Decode(&rel); err != nil {
		return nil, apierrors.NewAPIError("failed to parse release response", err)
	}
	return &rel, nil
}
