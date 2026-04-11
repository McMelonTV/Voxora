package libspotdl

import (
	"bytes"
	"testing"

	metadatapb "github.com/devgianlu/go-librespot/proto/spotify/metadata"
)

func TestTrackBaseName(t *testing.T) {
	got := trackBaseName([]string{"Artist A", "Artist B", "Artist C", "Artist D"}, "Song.Name")
	want := "Artist A, Artist B, Artist C, and others - SongName"
	if got != want {
		t.Fatalf("trackBaseName = %q, want %q", got, want)
	}
}

func TestEpisodeBaseName(t *testing.T) {
	got := episodeBaseName("My Show", `Episode: "One"`)
	want := "My Show - Episode One"
	if got != want {
		t.Fatalf("episodeBaseName = %q, want %q", got, want)
	}
}

func TestSelectBestAudioFile(t *testing.T) {
	ogg160 := metadatapb.AudioFile_OGG_VORBIS_160
	ogg320 := metadatapb.AudioFile_OGG_VORBIS_320
	mp3320 := metadatapb.AudioFile_MP3_320

	files := []*metadatapb.AudioFile{
		{FileId: []byte{1}, Format: &ogg160},
		{FileId: []byte{2}, Format: &mp3320},
		{FileId: []byte{3}, Format: &ogg320},
	}

	selected := selectBestAudioFile(files, 320)
	if selected == nil {
		t.Fatal("expected a selected file")
	}
	if selected.GetFormat() != metadatapb.AudioFile_MP3_320 {
		t.Fatalf("selected format = %s, want %s", selected.GetFormat(), metadatapb.AudioFile_MP3_320)
	}
}

func TestResolveOutputPathAddsExtension(t *testing.T) {
	path, err := resolveOutputPath(Request{OutputPath: "/tmp/test-track"}, MediaMetadata{FileExtension: "ogg"}, 1)
	if err != nil {
		t.Fatalf("resolveOutputPath returned error: %v", err)
	}
	if path != "/tmp/test-track.ogg" {
		t.Fatalf("path = %q, want %q", path, "/tmp/test-track.ogg")
	}
}

func TestResolveOutputPathUsesRequestedFormatExtension(t *testing.T) {
	path, err := resolveOutputPath(Request{OutputPath: "/tmp/test-track", OutputFormat: OutputFormatMP3}, MediaMetadata{FileExtension: "ogg"}, 1)
	if err != nil {
		t.Fatalf("resolveOutputPath returned error: %v", err)
	}
	if path != "/tmp/test-track.mp3" {
		t.Fatalf("path = %q, want %q", path, "/tmp/test-track.mp3")
	}
}

func TestNormalizeOutputFormat(t *testing.T) {
	if got := normalizeOutputFormat(""); got != OutputFormatSource {
		t.Fatalf("normalizeOutputFormat(\"\") = %q, want %q", got, OutputFormatSource)
	}
	if got := normalizeOutputFormat(OutputFormatFLAC); got != OutputFormatFLAC {
		t.Fatalf("normalizeOutputFormat(flac) = %q, want %q", got, OutputFormatFLAC)
	}
	if got := normalizeOutputFormat("wav"); got != "" {
		t.Fatalf("normalizeOutputFormat(wav) = %q, want empty", got)
	}
}

func TestDetectOggPayloadOffsetPrefersSpotifyHeaderOffset(t *testing.T) {
	data := make([]byte, 0xA7+4)
	copy(data[0:], []byte("OggS"))
	copy(data[0xA7:], []byte("OggS"))

	offset := detectOggPayloadOffset(bytes.NewReader(data), int64(len(data)))
	if offset != 0xA7 {
		t.Fatalf("detectOggPayloadOffset = %d, want %d", offset, 0xA7)
	}
}

func TestDetectOggPayloadOffsetFallsBackToStart(t *testing.T) {
	data := make([]byte, 128)
	copy(data[0:], []byte("OggS"))

	offset := detectOggPayloadOffset(bytes.NewReader(data), int64(len(data)))
	if offset != 0 {
		t.Fatalf("detectOggPayloadOffset = %d, want 0", offset)
	}
}
