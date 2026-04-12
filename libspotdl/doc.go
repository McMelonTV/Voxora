// Package libspotdl provides the Spotify audio download core for Voxora.
//
// # Scope
//
// This package intentionally focuses on the downloader pipeline rather than
// cloning the entire upstream spotify-dl-on-steroids application surface.
// It can:
//   - authenticate with Spotify using go-librespot,
//   - resolve Spotify track/episode URLs and URIs,
//   - expand album/playlist/show inputs into downloadable items,
//   - fetch and decrypt the selected Spotify audio file,
//   - optionally transcode file outputs to MP3 or FLAC via ffmpeg,
//   - embed text tags and artwork for supported file outputs, and
//   - write either the original downloaded source container or a post-processed
//     file to disk. Writer mode remains raw-source only.
//
// The package still intentionally focuses on the downloader pipeline rather
// than reproducing the entire upstream CLI application surface.
package libspotdl
