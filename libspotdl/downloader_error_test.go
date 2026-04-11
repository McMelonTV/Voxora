package libspotdl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/devgianlu/go-librespot/audio"
)

func TestWithAudioKeyTroubleshootingPremiumHint(t *testing.T) {
	err := withAudioKeyTroubleshooting(errors.New("failed retrieving aes key with code 1"))
	if err == nil {
		t.Fatal("expected an error")
	}
	message := err.Error()
	if !strings.Contains(message, "Spotify Premium") {
		t.Fatalf("expected Spotify Premium hint, got: %s", message)
	}
	if !strings.Contains(message, "interactive track playback/downloads") {
		t.Fatalf("expected account eligibility hint, got: %s", message)
	}
}

func TestRequestAudioKeyWithRetryRetriesCode1(t *testing.T) {
	t.Parallel()

	attempts := 0
	want := []byte("0123456789abcdef")
	got, err := requestAudioKeyWithRetry(context.Background(), newDefaultLogger(), func(context.Context) ([]byte, error) {
		attempts++
		if attempts < 3 {
			return nil, &audio.KeyProviderError{Code: 1}
		}
		return want, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if string(got) != string(want) {
		t.Fatalf("unexpected key bytes")
	}
}

func TestRequestAudioKeyWithRetryDoesNotRetryOtherCodes(t *testing.T) {
	t.Parallel()

	attempts := 0
	_, err := requestAudioKeyWithRetry(context.Background(), newDefaultLogger(), func(context.Context) ([]byte, error) {
		attempts++
		return nil, &audio.KeyProviderError{Code: 2}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
}
