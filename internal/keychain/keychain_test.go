package keychain

import (
	"errors"
	"runtime"
	"testing"
)

func TestRead_NonDarwin_ReturnsUnsupported(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("test exercises the !darwin stub")
	}
	_, err := New().Read(t.Context(), "any-service", "any-account")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(ErrNotFound, ErrAccessDenied) ||
		errors.Is(ErrAccessDenied, ErrUnsupported) {
		t.Fatal("sentinels must not alias each other")
	}
}
