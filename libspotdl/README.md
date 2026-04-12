# libspotd

`libspotd` is a Go library module for Voxora that reimplements the **actual Spotify download pipeline** from the upstream Rust project [`spotify-dl-on-steroids`](../spotify-dl-on-steroids), while intentionally **not** cloning the whole CLI app.

## Current scope

Implemented:
- Spotify authentication with cached stored credentials
- Track and episode downloads
- Album, playlist, and show expansion into downloadable items
- CDN resolution, AES key retrieval, stream decryption, and file writing
- Optional file post-processing with `ffmpeg`
  - transcoding to `mp3` or `flac`
  - textual tags
  - embedded artwork where the target format supports it well
- Progress callbacks and a tiny example CLI

Still intentionally out of scope:
- full CLI parity with the Rust project
- playlist history/sync cache behavior
- Android/mobile bundling of `ffmpeg`

## Requirements

For real track downloads, a Spotify account that is allowed to perform interactive playback is required. In practice, this generally means **Spotify Premium**.

For raw-source downloads streamed into an `io.Writer`, no post-processing tool is required.

For file outputs that use tagging, artwork, or transcoding, `ffmpeg` must be available:

```bash
ffmpeg -version
```

## Package path

```go
import "github.com/McMelonTV/Voxora/libspotdl"
```

## Library example

```go
ctx := context.Background()

d, err := libspotd.New(ctx, libspotd.Config{
    FFmpegPath: "ffmpeg",
})
if err != nil {
    log.Fatal(err)
}
defer d.Close()

results, err := d.Download(ctx, libspotd.Request{
    Source:       "https://open.spotify.com/track/11dFghVXANMlKmJXsNCbNl",
    OutputDir:    "downloads",
    OutputFormat: libspotd.OutputFormatMP3,
    Tagging: libspotd.TaggingConfig{
        Enabled:        true,
        IncludeArtwork: true,
    },
    Progress: func(p libspotd.Progress) {
        fmt.Printf("[%d/%d] %s %s\n", p.ItemIndex+1, p.ItemCount, p.Stage, p.URI)
    },
})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("downloaded %d item(s)\n", len(results))
```

## Tiny runner

A small helper CLI is included under `cmd/spotd-fetch`.

Default behavior of the runner:
- output format: `mp3`
- tagging: enabled
- artwork: enabled

Example:

```bash
go run ./cmd/spotd-fetch -out ./downloads "https://open.spotify.com/track/11dFghVXANMlKmJXsNCbNl"
```

If you want to force a fresh login instead of reusing cached stored credentials:

```bash
go run ./cmd/spotd-fetch -ignore-cache-auth -out ./downloads "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

If you want to delete the cache first and then re-authenticate:

```bash
go run ./cmd/spotd-fetch -reset-auth -out ./downloads "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

Use the original Spotify source container instead:

```bash
go run ./cmd/spotd-fetch -format source -out ./downloads "https://open.spotify.com/track/11dFghVXANMlKmJXsNCbNl"
```

Use FLAC output instead:

```bash
go run ./cmd/spotd-fetch -format flac -out ./downloads "https://open.spotify.com/track/11dFghVXANMlKmJXsNCbNl"
```

## Authentication

`libspotd` authenticates in this order:

1. **Explicit token auth** if you provide `Username` + `AccessToken`
2. **Cached stored credentials** from the credentials cache file
3. **Interactive browser login** if neither of the above is available

### Credentials cache location

By default, credentials are stored at:

```text
$XDG_CONFIG_HOME/libspotd/credentials.json
```

If `XDG_CONFIG_HOME` is not set, Go resolves it from your user config directory, typically:

```text
~/.config/libspotd/credentials.json
```

You can override it in the runner:

```bash
go run ./cmd/spotd-fetch -credentials /path/to/credentials.json -out ./downloads "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

### Interactive login flow

If no valid cached credentials exist and you do not pass a token, `libspotd` starts the Spotify OAuth login flow and prints the authorization URL via the default logger.

Typical usage:

```bash
go run ./cmd/spotd-fetch -out ./downloads "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

Then:
- open the printed URL in your browser
- log into Spotify
- allow access
- let Spotify redirect back to `http://127.0.0.1:<port>/login`

After that, the stored credentials are cached and reused on later runs.

You can change the callback port if needed:

```bash
go run ./cmd/spotd-fetch -callback-port 36842 -out ./downloads "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

### Explicit token auth

If you already have a Spotify access token and username, you can avoid the interactive flow:

```bash
go run ./cmd/spotd-fetch \
  -username your_spotify_username \
  -access-token YOUR_SPOTIFY_ACCESS_TOKEN \
  -out ./downloads \
  "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

In library code, that maps to:

```go
libspotd.Config{
    Auth: libspotd.AuthConfig{
        Username:    "your_spotify_username",
        AccessToken: "YOUR_SPOTIFY_ACCESS_TOKEN",
    },
}
```

### Common auth failure: `apresolve.spotify.com` goes to `0.0.0.0`

If you see an error like this:

```text
failed getting accesspoint from resolver: failed fetching apresolve URL: Get "https://apresolve.spotify.com/?type=accesspoint": dial tcp 0.0.0.0:443: connect: connection refused
```

that usually means your machine is blocking Spotify's resolver host before `libspotd` ever gets to the login step.

Common causes:
- `/etc/hosts` contains a line like `0.0.0.0 apresolve.spotify.com`
- DNS sinkhole / adblock rules
- VPN or firewall filtering
- broken proxy environment variables

Check these first:

```bash
getent hosts apresolve.spotify.com
grep -n 'spotify\|apresolve' /etc/hosts
env | grep -Ei '^(http|https|all|no)_proxy='
```

`libspotd` cannot authenticate until `apresolve.spotify.com` resolves to a real Spotify address.

### Common playback failure: `failed retrieving aes key with code 1`

If authentication succeeds but the download later fails like this:

```text
open raw stream: retrieve audio key: failed retrieving aes key with code 1
```

then networking and login are already working. The failure has moved to Spotify denying the track decryption key.

That usually means the account is **not allowed to perform interactive full-track playback/downloads** for librespot-compatible clients. In practice, this generally means you need **Spotify Premium** for full downloads.

If you are sure the account is Premium, the next practical check is to force a fresh login and make sure `libspotd` is not reusing stale cached credentials from a different account:

```bash
go run ./cmd/spotd-fetch -reset-auth -ignore-cache-auth -out ./downloads "spotify:track:11dFghVXANMlKmJXsNCbNl"
```

## Notes on output behavior

- `OutputFormatSource` keeps the original downloaded Spotify source container.
- `OutputFormatMP3` and `OutputFormatFLAC` use `ffmpeg` to transcode.
- `Writer` output mode bypasses post-processing and only writes the raw downloaded source stream.
- Artwork embedding is strongest on `mp3` and `flac` outputs.

## Testing

```bash
go test ./...
```

The test suite includes unit coverage plus local `ffmpeg`-based post-processing tests that do not require live Spotify credentials.


