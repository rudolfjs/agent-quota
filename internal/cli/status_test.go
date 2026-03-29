package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/schnetlerr/agent-quota/internal/provider"
)

type statusProvider struct {
	name      string
	available bool
	called    bool
	err       error
}

func (p *statusProvider) Name() string    { return p.name }
func (p *statusProvider) Available() bool { return p.available }
func (p *statusProvider) FetchQuota(_ context.Context) (provider.QuotaResult, error) {
	p.called = true
	if p.err != nil {
		return provider.QuotaResult{}, p.err
	}
	return provider.QuotaResult{Provider: p.name, Status: "ok"}, nil
}

func TestFetchAll_marksUnavailableWithoutFetching(t *testing.T) {
	providers := []provider.Provider{
		&statusProvider{name: "claude", available: false},
		&statusProvider{name: "gemini", available: true},
	}

	results := fetchAll(t.Context(), providers)
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Status != "unavailable" {
		t.Fatalf("results[0].Status = %q, want %q", results[0].Status, "unavailable")
	}
	if results[1].Status != "ok" {
		t.Fatalf("results[1].Status = %q, want %q", results[1].Status, "ok")
	}

	first := providers[0].(*statusProvider)
	if first.called {
		t.Fatal("FetchQuota() called for unavailable provider")
	}
}

func TestFetchAll_marksErrors(t *testing.T) {
	providers := []provider.Provider{
		&statusProvider{name: "openai", available: true, err: errors.New("boom")},
	}

	results := fetchAll(t.Context(), providers)
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Status != "error" {
		t.Fatalf("results[0].Status = %q, want %q", results[0].Status, "error")
	}
}
