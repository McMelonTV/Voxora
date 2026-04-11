package libspotdl

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	librespot "github.com/devgianlu/go-librespot"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
	librespotsession "github.com/devgianlu/go-librespot/session"
	"golang.org/x/oauth2"
	spotifyoauth2 "golang.org/x/oauth2/spotify"
)

const defaultCallbackPort = 36842

type credentialCache struct {
	DeviceID          string `json:"device_id"`
	Username          string `json:"username"`
	StoredCredentials string `json:"stored_credentials"`
}

type spotifyTokenAuth struct {
	Username string
	Token    string
}

func defaultCredentialsFile() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}

	return filepath.Join(configDir, "libspotdl", "credentials.json"), nil
}

func resolveCredentialsFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return defaultCredentialsFile()
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	return filepath.Clean(path), nil
}

// ResolveCredentialsFile returns the effective credentials cache path that
// libspotdl will use for stored credentials and device ID caching.
func ResolveCredentialsFile(path string) (string, error) {
	return resolveCredentialsFile(path)
}

func randomDeviceID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}

	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4],
		buf[4:6],
		buf[6:8],
		buf[8:10],
		buf[10:16],
	), nil
}

func isLegacyHexDeviceID(deviceID string) bool {
	deviceID = strings.TrimSpace(deviceID)
	if len(deviceID) != 40 {
		return false
	}
	_, err := hex.DecodeString(deviceID)
	return err == nil
}

func loadCredentialCache(path string) (*credentialCache, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials cache: %w", err)
	}

	var cache credentialCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("decode credentials cache: %w", err)
	}

	return &cache, nil
}

func saveCredentialCache(path, deviceID string, sess *librespotsession.Session) error {
	cache := credentialCache{
		DeviceID:          deviceID,
		Username:          sess.Username(),
		StoredCredentials: base64.StdEncoding.EncodeToString(sess.StoredCredentials()),
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials cache: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create credentials cache dir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials cache: %w", err)
	}

	return nil
}

func withSessionTroubleshooting(err error) error {
	if err == nil {
		return nil
	}

	message := err.Error()
	if strings.Contains(message, "apresolve.spotify.com") && strings.Contains(message, "0.0.0.0:443") {
		return fmt.Errorf("%w\n\nTroubleshooting: apresolve.spotify.com is resolving to 0.0.0.0. Check /etc/hosts, DNS sinkhole/adblock rules, VPN filters, or firewall tooling blocking Spotify resolver hosts. libspotdl cannot authenticate until apresolve.spotify.com resolves to a real Spotify address", err)
	}

	if strings.Contains(message, "apresolve.spotify.com") && strings.Contains(strings.ToLower(message), "proxy") {
		return fmt.Errorf("%w\n\nTroubleshooting: check HTTP_PROXY, HTTPS_PROXY, ALL_PROXY, and NO_PROXY. A broken proxy setting can prevent libspotdl from reaching Spotify's resolver", err)
	}

	return err
}

func acquireInteractiveSpotifyToken(ctx context.Context, log librespot.Logger, callbackPort int) (*spotifyTokenAuth, error) {
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	callbackPort, codeCh, err := librespotsession.NewOAuth2Server(serverCtx, log, callbackPort)
	if err != nil {
		return nil, fmt.Errorf("failed initializing oauth2 server: %w", err)
	}

	oauthConf := &oauth2.Config{
		ClientID:    librespot.ClientIdHex,
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", callbackPort),
		Scopes:      []string{"streaming"},
		Endpoint:    spotifyoauth2.Endpoint,
	}

	verifier := oauth2.GenerateVerifier()
	authURL := oauthConf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))
	log.Infof("to complete authentication visit the following link: %s", authURL)

	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case code = <-codeCh:
	}
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("spotify oauth callback returned no authorization code")
	}

	token, err := oauthConf.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("failed exchanging oauth2 code: %w", err)
	}

	username, _ := token.Extra("username").(string)
	username = strings.TrimSpace(username)

	accessToken := strings.TrimSpace(token.AccessToken)
	if accessToken == "" {
		return nil, errors.New("spotify oauth token response did not include an access token")
	}

	return &spotifyTokenAuth{Username: username, Token: accessToken}, nil
}

func resolveSessionCredentials(ctx context.Context, cfg Config, log librespot.Logger, client *http.Client, deviceID string, cache *credentialCache) (any, error) {
	switch {
	case strings.TrimSpace(cfg.Auth.AccessToken) != "":
		return librespotsession.SpotifyTokenCredentials{
			Username: strings.TrimSpace(cfg.Auth.Username),
			Token:    strings.TrimSpace(cfg.Auth.AccessToken),
		}, nil
	case !cfg.Auth.IgnoreStoredCredentials && cache != nil && cache.Username != "" && cache.StoredCredentials != "":
		stored, err := base64.StdEncoding.DecodeString(cache.StoredCredentials)
		if err != nil {
			return nil, fmt.Errorf("decode cached stored credentials: %w", err)
		}
		return librespotsession.StoredCredentials{
			Username: cache.Username,
			Data:     stored,
		}, nil
	default:
		callbackPort := cfg.Auth.CallbackPort
		if callbackPort == 0 {
			callbackPort = defaultCallbackPort
		}

		auth, err := acquireInteractiveSpotifyToken(ctx, log, callbackPort)
		if err != nil {
			return nil, err
		}

		return librespotsession.SpotifyTokenCredentials{
			// Match Rust librespot behavior: Spotify token auth does not require
			// sending username in AP login credentials.
			Username: "",
			Token:    auth.Token,
		}, nil
	}
}

func newAuthenticatedSession(ctx context.Context, cfg Config, log librespot.Logger, client *http.Client) (*librespotsession.Session, error) {
	credentialsFile, err := resolveCredentialsFile(cfg.Auth.CredentialsFile)
	if err != nil {
		return nil, err
	}

	cache, err := loadCredentialCache(credentialsFile)
	if err != nil {
		return nil, err
	}

	deviceID := strings.TrimSpace(cfg.Auth.DeviceID)
	if deviceID == "" && cache != nil && !isLegacyHexDeviceID(cache.DeviceID) {
		deviceID = strings.TrimSpace(cache.DeviceID)
	}
	if deviceID == "" {
		deviceID, err = randomDeviceID()
		if err != nil {
			return nil, err
		}
	}

	creds, err := resolveSessionCredentials(ctx, cfg, log, client, deviceID, cache)
	if err != nil {
		return nil, withSessionTroubleshooting(err)
	}

	sess, err := librespotsession.NewSessionFromOptions(ctx, &librespotsession.Options{
		Log:         log,
		Client:      client,
		DeviceType:  devicespb.DeviceType_COMPUTER,
		DeviceId:    deviceID,
		Credentials: creds,
	})
	if err != nil {
		return nil, fmt.Errorf("create spotify session: %w", withSessionTroubleshooting(err))
	}

	if err := saveCredentialCache(credentialsFile, deviceID, sess); err != nil {
		sess.Close()
		return nil, err
	}

	return sess, nil
}
