package libspotdl

import (
	"io"
	"net/http"
	"time"

	librespot "github.com/devgianlu/go-librespot"
)

type ItemKind string

const (
	ItemKindTrack    ItemKind = "track"
	ItemKindEpisode  ItemKind = "episode"
	ItemKindAlbum    ItemKind = "album"
	ItemKindPlaylist ItemKind = "playlist"
	ItemKindShow     ItemKind = "show"
)

type OutputFormat string

const (
	OutputFormatSource OutputFormat = "source"
	OutputFormatMP3    OutputFormat = "mp3"
	OutputFormatFLAC   OutputFormat = "flac"
)

type TaggingConfig struct {
	// Enabled controls whether textual metadata is written during file output.
	Enabled bool

	// IncludeArtwork controls whether album/show artwork is embedded when the
	// selected output container supports it best-effort via ffmpeg.
	IncludeArtwork bool
}

type AuthConfig struct {
	// CredentialsFile stores cached go-librespot stored credentials and the
	// generated device ID. When empty, a default file under the user's config
	// directory is used.
	CredentialsFile string

	// DeviceID overrides the generated or cached Spotify device ID.
	DeviceID string

	// Username and AccessToken allow the downloader to authenticate without the
	// interactive browser flow. Username is required when AccessToken is set.
	Username    string
	AccessToken string

	// CallbackPort controls the interactive OAuth callback listener. Zero uses a
	// sensible default.
	CallbackPort int

	// IgnoreStoredCredentials skips the cached stored-credentials path so callers
	// can force a fresh token-based or interactive login.
	IgnoreStoredCredentials bool
}

type Config struct {
	Auth AuthConfig

	// HTTPClient overrides the default client used for Spotify and CDN requests.
	HTTPClient *http.Client

	// Logger receives internal library logs. When nil, a default stderr logger is used.
	Logger librespot.Logger

	// PreferredBitrate chooses the closest supported source bitrate. Zero uses
	// the default of 320 kbps.
	PreferredBitrate int

	// FFmpegPath overrides the ffmpeg executable used for tagging/transcoding.
	// When empty, "ffmpeg" is resolved from PATH.
	FFmpegPath string
}

type Request struct {
	// Source is a Spotify URI or URL pointing to a track, episode, album,
	// playlist, or show.
	Source string

	// OutputDir is used when OutputPath is empty and Writer is nil.
	OutputDir string

	// OutputPath writes a single resolved item to an explicit path. If the path
	// has no extension, the selected final output extension is appended.
	OutputPath string

	// Writer allows streaming a single resolved item directly to a caller-owned
	// sink instead of creating a file.
	Writer io.Writer

	// Overwrite controls whether existing output files should be replaced.
	Overwrite bool

	// OutputFormat selects the final file format for file outputs. The zero value
	// uses the original downloaded Spotify source container.
	OutputFormat OutputFormat

	// Tagging controls metadata and artwork embedding for file outputs.
	Tagging TaggingConfig

	// Progress receives best-effort progress updates for the active item.
	Progress func(Progress)
}

type ResolvedItem struct {
	URI   string
	Kind  ItemKind
	Index int
}

type MediaMetadata struct {
	URI              string
	Kind             ItemKind
	Title            string
	Artists          []string
	Album            string
	Show             string
	ReleaseDate      string
	TrackNumber      int
	DiscNumber       int
	ArtworkURL       string
	Duration         time.Duration
	SourceFormat     string
	FileExtension    string
	ContentType      string
	DefaultBaseName  string
	DefaultFileName  string
	PreferredBitrate int
}

type Progress struct {
	URI          string
	Kind         ItemKind
	ItemIndex    int
	ItemCount    int
	Stage        string
	BytesWritten int64
	TotalBytes   int64
	OutputPath   string
}

type Result struct {
	URI          string
	Kind         ItemKind
	OutputPath   string
	BytesWritten int64
	Skipped      bool
	OutputFormat OutputFormat
	Metadata     MediaMetadata
}

// UserPlaylistSummary describes one playlist from the authenticated account.
type UserPlaylistSummary struct {
	ID          string
	URI         string
	Name        string
	Description string
	OwnerName   string
	TrackCount  int
	ImageURL    string
	Public      bool
}

// LikedSongsSummary describes the account's "Liked Songs" collection.
type LikedSongsSummary struct {
	URI        string
	Name       string
	TrackCount int
}

// LibrarySnapshot contains account playlist data and liked songs summary loaded
// from one authenticated session.
type LibrarySnapshot struct {
	Playlists      []UserPlaylistSummary
	Liked          LikedSongsSummary
	LikedAvailable bool
}

// LibraryTrackSummary is a compact display model for one track in a playlist
// or collection view.
type LibraryTrackSummary struct {
	URI            string
	Name           string
	ArtistText     string
	Downloaded     bool
	DownloadedPath string
}

// LibraryTrackPage is a paged slice of context tracks for incremental loading.
type LibraryTrackPage struct {
	Items   []LibraryTrackSummary
	Offset  int
	Limit   int
	Total   int
	HasMore bool
}
