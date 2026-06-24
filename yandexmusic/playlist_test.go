package yandexmusic

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaylist(t *testing.T) {
	htmlFile, err := os.ReadFile(playlistFile)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(htmlFile)
	}))
	defer server.Close()

	playlist := NewPlaylist("", 0)
	playlist.SourceURL = server.URL + "/playlists/" + playlistID

	require.NoError(t, playlist.GetTracks())
	assert.Equal(t, "music-blog", playlist.Owner)
	assert.Equal(t, int64(2324), playlist.PlaylistID)
	assert.Equal(t, playlistID, playlist.UUID)
	assert.Equal(t, "Змеиный плейлист", playlist.Title)
	require.Len(t, playlist.Tracks, 6)
	assert.Equal(t, "65093274", playlist.Tracks[0].ID)
	assert.Equal(t, "На кончике хвоста", playlist.Tracks[0].Title)
}

func TestNewPlaylistFromLink(t *testing.T) {
	playlist, err := NewPlaylistFromLink("https://music.yandex.ru/playlists/" + playlistID + "?utm=test")
	require.NoError(t, err)
	require.NotNil(t, playlist)
	assert.Equal(t, playlistID, playlist.UUID)
	assert.Equal(t, "https://music.yandex.ru/playlists/"+playlistID, playlist.SourceURL)

	playlist, err = NewPlaylistFromLink("https://music.yandex.ru/users/music-blog/playlists/2324")
	require.NoError(t, err)
	require.NotNil(t, playlist)
	assert.Equal(t, "music-blog", playlist.Owner)
	assert.Equal(t, int64(2324), playlist.PlaylistID)

	playlist, err = NewPlaylistFromLink("/playlists/" + playlistID)
	require.NoError(t, err)
	require.NotNil(t, playlist)
	assert.Equal(t, playlistID, playlist.UUID)
	assert.Equal(t, "https://music.yandex.ru/playlists/"+playlistID, playlist.SourceURL)
}
