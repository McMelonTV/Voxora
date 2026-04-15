package libspotdl

import "testing"

func TestBestSpotifySearchImageURLPrefersLargestArea(t *testing.T) {
	got := bestSpotifySearchImageURL([]spotifySearchImage{
		{URL: "https://img/small", Width: 64, Height: 64},
		{URL: "https://img/large", Width: 640, Height: 640},
		{URL: "https://img/medium", Width: 300, Height: 300},
	})

	if got != "https://img/large" {
		t.Fatalf("bestSpotifySearchImageURL() = %q, want %q", got, "https://img/large")
	}
}

func TestBestSpotifySearchImageURLFallsBackToFirstNonEmpty(t *testing.T) {
	got := bestSpotifySearchImageURL([]spotifySearchImage{{URL: " https://img/only "}})

	if got != "https://img/only" {
		t.Fatalf("bestSpotifySearchImageURL() = %q, want %q", got, "https://img/only")
	}
}

func TestBestSpotifySearchImageURLEmpty(t *testing.T) {
	got := bestSpotifySearchImageURL(nil)
	if got != "" {
		t.Fatalf("bestSpotifySearchImageURL() = %q, want empty string", got)
	}
}
