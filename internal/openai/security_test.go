package openai_test

// Security regression tests for the OpenAI provider.

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/openai"
)

// validOpenAIAuthPayload returns a well-formed auth file payload.
func validOpenAIAuthPayload() map[string]any {
	return map[string]any{
		"auth_mode": "oauth",
		"tokens": map[string]any{
			"access_token":  "tok_valid",
			"refresh_token": "ref_valid",
		},
	}
}

// TestFetchQuota_oversizedResponse_returnsError verifies the 1 MiB response
// body limit is enforced against a malicious/misbehaving server.
func TestFetchQuota_oversizedResponse_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		padding := strings.Repeat("x", (1<<20)+128)
		_, _ = fmt.Fprintf(w, `{"overflow":"%s"}`, padding)
	}))
	defer srv.Close()

	dir := t.TempDir()
	authPath := writeAuthFile(t, dir, validOpenAIAuthPayload())
	p := openai.New(
		openai.WithAuthPath(authPath),
		openai.WithHTTPClient(&http.Client{}),
		openai.WithUsageURL(srv.URL+"/backend-api/wham/usage"),
		openai.WithTokenURL(srv.URL+"/token"),
	)
	_, err := p.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}

// TestFetchQuota_allErrorPaths_returnDomainError verifies every error path in
// OpenAI's FetchQuota returns a *DomainError.
func TestFetchQuota_allErrorPaths_returnDomainError(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "http_500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "malformed_json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{bad`))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			dir := t.TempDir()
			authPath := writeAuthFile(t, dir, validOpenAIAuthPayload())
			p := openai.New(
				openai.WithAuthPath(authPath),
				openai.WithHTTPClient(&http.Client{}),
				openai.WithUsageURL(srv.URL+"/backend-api/wham/usage"),
				openai.WithTokenURL(srv.URL+"/token"),
			)
			_, err := p.FetchQuota(t.Context())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var domErr *apierrors.DomainError
			if !errors.As(err, &domErr) {
				t.Errorf("expected *DomainError, got %T: %v", err, err)
			}
		})
	}
}
