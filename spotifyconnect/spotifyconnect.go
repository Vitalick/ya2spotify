// Package spotifyconnect запускает локальный web UI, выполняет Spotify OAuth и переносит найденные треки в плейлист.
package spotifyconnect

import (
	b64 "encoding/base64"
	"flag"
	"fmt"

	"github.com/valyala/fasthttp"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
)

const (
	callbackURI = "/callback"
	//state = "spotifyApi"
)

var (
	port          int
	spotifyID     string
	spotifySecret string
)

func init() {
	flag.IntVar(&port, "p", 3500, "port for webserver")
	flag.StringVar(&spotifyID, "id", "", "spotify id")
	flag.StringVar(&spotifySecret, "secret", "", "spotify secret")
	if spotifyID+spotifySecret == "" {
		spotifyID = "329b37a12f1d484ca5a2b85e91ecae83"
		spotifySecret = "VmpKMGExTnRWbkpPVldoVVlsaFNUMVJYZUV0aU1WRjRWV3h3YTAxRVZrWlZWbEpYV1ZaSmQxZHVVbFZpYmtKSFZGVlZNVk5HVWxsYVJUVlhUVWQwTlZZeFdsTlZNa1pHVFZoU1ZGWkVRVGs9"
		for i := 0; i < 5; i++ {
			sDec, _ := b64.StdEncoding.DecodeString(spotifySecret)
			spotifySecret = string(sDec)
		}
	}
}

// redirectURL собирает callback URL для Spotify OAuth на локальном сервере.
//
// Returns:
//   - string: полный URL callback-обработчика с текущим портом.
func redirectURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackURI)
}

// Start создает Spotify OAuth-клиент и запускает HTTP-сервер приложения.
//
// Returns:
//   - error: ошибка запуска fasthttp-сервера.
func Start() error {
	auth := spotifyauth.New(spotifyauth.WithRedirectURL(redirectURL()), spotifyauth.WithScopes(
		spotifyauth.ScopeUserReadPlaybackState,
		spotifyauth.ScopeUserReadCurrentlyPlaying,
		spotifyauth.ScopeUserReadRecentlyPlayed,
		spotifyauth.ScopeUserLibraryRead,
		spotifyauth.ScopeUserLibraryModify,
		spotifyauth.ScopeUserModifyPlaybackState,
		spotifyauth.ScopeUserReadPrivate,
		spotifyauth.ScopePlaylistModifyPublic,
		spotifyauth.ScopePlaylistModifyPrivate,
		spotifyauth.ScopePlaylistReadPrivate,
		spotifyauth.ScopePlaylistReadCollaborative,
	), spotifyauth.WithClientID(spotifyID), spotifyauth.WithClientSecret(spotifySecret))
	//url := auth.AuthURL(state)
	s := newServer(auth, "spotifyApi")
	fmt.Printf("Starting at http://127.0.0.1:%d/\n", port)
	fmt.Printf("Redirect uri %s\n", redirectURL())
	return fasthttp.ListenAndServe(fmt.Sprintf(":%d", port), s.ServeHTTP)
}
