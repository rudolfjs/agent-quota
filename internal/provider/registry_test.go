package provider_test

import (
	"context"
	"testing"

	"github.com/schnetlerr/agent-quota/internal/provider"
)

// fakeProvider is a test double for the Provider interface.
type fakeProvider struct {
	name      string
	available bool
}

func (f *fakeProvider) Name() string    { return f.name }
func (f *fakeProvider) Available() bool { return f.available }
func (f *fakeProvider) FetchQuota(_ context.Context) (provider.QuotaResult, error) {
	return provider.QuotaResult{Provider: f.name, Status: "ok"}, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := provider.NewRegistry()
	p := &fakeProvider{name: "claude", available: true}
	reg.Register(p)

	got, ok := reg.Get("claude")
	if !ok {
		t.Fatal("Get(claude) returned false")
	}
	if got.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", got.Name(), "claude")
	}
}

func TestRegistry_Get_missingProvider(t *testing.T) {
	reg := provider.NewRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("Get on missing provider should return false")
	}
}

func TestRegistry_Available_filtersUnavailable(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "claude", available: true})
	reg.Register(&fakeProvider{name: "openai", available: false})
	reg.Register(&fakeProvider{name: "gemini", available: true})

	avail := reg.Available()
	if len(avail) != 2 {
		t.Errorf("Available() returned %d providers, want 2", len(avail))
	}
	for _, p := range avail {
		if !p.Available() {
			t.Errorf("Available() included unavailable provider %q", p.Name())
		}
	}
}

func TestRegistry_All_returnsAll(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "a", available: true})
	reg.Register(&fakeProvider{name: "b", available: false})

	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d providers, want 2", len(all))
	}
}

func TestRegistry_Register_duplicateOverwrites(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(&fakeProvider{name: "claude", available: false})
	reg.Register(&fakeProvider{name: "claude", available: true})

	p, ok := reg.Get("claude")
	if !ok {
		t.Fatal("Get returned false")
	}
	if !p.Available() {
		t.Error("second Register should overwrite first; want available=true")
	}
}
