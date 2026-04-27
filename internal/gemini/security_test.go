package gemini_test

// Security regression tests for the Gemini provider.

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apierrors "github.com/rudolfjs/agent-quota/internal/errors"
	"github.com/rudolfjs/agent-quota/internal/gemini"
)

// validGeminiCredsPayload returns a credential payload with a non-expired token.
func validGeminiCredsPayload() map[string]any {
	return map[string]any{
		"access_token":  "tok_valid",
		"refresh_token": "ref_valid",
		"expiry_date":   time.Now().Add(time.Hour).UnixMilli(),
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
	credPath := writeCredFile(t, dir, validGeminiCredsPayload())
	p := gemini.New(
		gemini.WithCredentialsPath(credPath),
		gemini.WithHTTPClient(&http.Client{}),
		gemini.WithBaseURL(srv.URL),
	)
	_, err := p.FetchQuota(t.Context())
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}

// TestFetchQuota_allErrorPaths_returnDomainError verifies every error path in
// Gemini's FetchQuota returns a *DomainError.
func TestFetchQuota_allErrorPaths_returnDomainError(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "loadCodeAssist_500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			name: "loadCodeAssist_401",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
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
			credPath := writeCredFile(t, dir, validGeminiCredsPayload())
			p := gemini.New(
				gemini.WithCredentialsPath(credPath),
				gemini.WithHTTPClient(&http.Client{}),
				gemini.WithBaseURL(srv.URL),
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
