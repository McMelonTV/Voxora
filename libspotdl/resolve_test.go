package libspotdl

import "testing"

func TestNormalizeSpotifyInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURI string
		want    ItemKind
	}{
		{
			name:    "track uri",
			input:   "spotify:track:11dFghVXANMlKmJXsNCbNl",
			wantURI: "spotify:track:11dFghVXANMlKmJXsNCbNl",
			want:    ItemKindTrack,
		},
		{
			name:    "playlist url",
			input:   "https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M?si=123",
			wantURI: "spotify:playlist:37i9dQZF1DXcBWIGoYBM5M",
			want:    ItemKindPlaylist,
		},
		{
			name:    "intl track url",
			input:   "https://open.spotify.com/intl-en/track/11dFghVXANMlKmJXsNCbNl",
			wantURI: "spotify:track:11dFghVXANMlKmJXsNCbNl",
			want:    ItemKindTrack,
		},
		{
			name:    "show url",
			input:   "https://open.spotify.com/show/4rOoJ6Egrf8K2IrywzwOMk",
			wantURI: "spotify:show:4rOoJ6Egrf8K2IrywzwOMk",
			want:    ItemKindShow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURI, gotKind, err := normalizeSpotifyInput(tt.input)
			if err != nil {
				t.Fatalf("normalizeSpotifyInput returned error: %v", err)
			}
			if gotURI != tt.wantURI {
				t.Fatalf("uri = %q, want %q", gotURI, tt.wantURI)
			}
			if gotKind != tt.want {
				t.Fatalf("kind = %q, want %q", gotKind, tt.want)
			}
		})
	}
}

func TestNormalizeSpotifyInputRejectsUnsupported(t *testing.T) {
	if _, _, err := normalizeSpotifyInput("https://example.com/not-spotify"); err == nil {
		t.Fatal("expected error for unsupported input")
	}
}
