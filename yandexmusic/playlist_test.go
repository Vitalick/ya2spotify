package yandexmusic

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const playlistID = "65b2e7a9-b735-e097-9b2c-ec6a89125c71"
const playlistURL = "https://music.yandex.ru/playlists/" + playlistID

func TestPlaylist(t *testing.T) {
	playlistFile, err := os.ReadFile("apiplaylists/" + playlistID + ".json")
	require.NoError(t, err)
	tracksFile, err := os.ReadFile("apiplaylists/" + playlistID + "_tracks.json")
	require.NoError(t, err)

	var tracksRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist/" + playlistID:
			assert.Equal(t, "false", r.URL.Query().Get("resumeStream"))
			assert.Equal(t, "false", r.URL.Query().Get("richTracks"))
			_, _ = w.Write(playlistFile)
		case "/tracks":
			tracksRequests++
			assert.Equal(t, playlistURL, r.Header.Get("X-Retpath-Y"))
			require.NoError(t, r.ParseMultipartForm(1<<20))
			assert.Equal(
				t,
				[]string{"65093274", "65093167", "65093357", "65092713", "65093322", "65093039"},
				r.MultipartForm.Value["trackIds"],
			)
			assert.Equal(t, []string{"false"}, r.MultipartForm.Value["removeDuplicates"])
			assert.Equal(t, []string{"true"}, r.MultipartForm.Value["withProgress"])
			_, _ = w.Write(tracksFile)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	restoreYandexAPIBaseURL := yandexMusicAPIBaseURL
	yandexMusicAPIBaseURL = server.URL
	defer func() {
		yandexMusicAPIBaseURL = restoreYandexAPIBaseURL
	}()

	playlist := NewPlaylist("", 0)
	playlist.SourceURL = playlistURL
	playlist.UUID = playlistID

	require.NoError(t, playlist.GetTracks())
	assert.Equal(t, "music-blog", playlist.Owner)
	assert.Equal(t, int64(2324), playlist.PlaylistID)
	assert.Equal(t, playlistID, playlist.UUID)
	assert.Equal(t, "Змеиный плейлист", playlist.Title)
	require.Len(t, playlist.Tracks, 6)
	assert.Equal(t, "65093274", playlist.Tracks[0].ID)
	assert.Equal(t, "На кончике хвоста", playlist.Tracks[0].Title)
	assert.Equal(t, 1, tracksRequests)
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

func TestYandexAPIPlaylistPathKeepsLKPrefix(t *testing.T) {
	playlist, err := NewPlaylistFromLink("https://music.yandex.ru/playlists/lk.be681207-ee6c-4b4b-9ba4-9297945bea37")
	require.NoError(t, err)
	require.NotNil(t, playlist)

	assert.Equal(t, "lk.be681207-ee6c-4b4b-9ba4-9297945bea37", playlist.UUID)
	assert.Equal(t, "/playlist/lk.be681207-ee6c-4b4b-9ba4-9297945bea37", playlist.apiPlaylistPath())
}

func TestDecodeYandexAPIPlaylistResponseNestedPlaylist(t *testing.T) {
	var response yandexAPIPlaylistResponse
	require.NoError(t, json.Unmarshal([]byte(`{
		"result": {
			"playlist": {
				"playlistUuid": "lk.be681207-ee6c-4b4b-9ba4-9297945bea37",
				"title": "Liked playlist",
				"trackIds": [99417193]
			}
		}
	}`), &response))

	assert.Equal(t, "Liked playlist", response.Result.Title)
	assert.Equal(t, []string{"99417193"}, response.Result.TrackIDs())
}

func TestYandexAPIPlaylistTrackIDs(t *testing.T) {
	var playlist yandexAPIPlaylist
	require.NoError(t, json.Unmarshal([]byte(`{
		"trackIds": ["99417193", 26219985],
		"tracks": [{"id": 1}]
	}`), &playlist))

	assert.Equal(t, []string{"99417193", "26219985"}, playlist.TrackIDs())

	require.NoError(t, json.Unmarshal([]byte(`{
		"tracks": [{"id": "99417193"}, {"id": 26219985}]
	}`), &playlist))

	assert.Equal(t, []string{"99417193", "26219985"}, playlist.TrackIDs())
}

func TestFetchYandexTracksBatchesTrackIDs(t *testing.T) {
	var requests [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/tracks", r.URL.Path)
		require.NoError(t, r.ParseMultipartForm(1<<20))
		trackIDs := append([]string(nil), r.MultipartForm.Value["trackIds"]...)
		requests = append(requests, trackIDs)

		tracks := make([]SingleTrack, 0, len(trackIDs))
		for _, trackID := range trackIDs {
			tracks = append(tracks, SingleTrack{ID: trackID, Title: "Track " + trackID})
		}
		require.NoError(t, json.NewEncoder(w).Encode(tracks))
	}))
	defer server.Close()
	restoreYandexAPIBaseURL := yandexMusicAPIBaseURL
	yandexMusicAPIBaseURL = server.URL
	defer func() {
		yandexMusicAPIBaseURL = restoreYandexAPIBaseURL
	}()

	trackIDs := make([]string, 0, yandexTrackIDsBatchSize+1)
	for i := range yandexTrackIDsBatchSize + 1 {
		trackIDs = append(trackIDs, strconv.Itoa(i+1))
	}

	tracks, err := fetchYandexTracks(trackIDs, playlistURL, nil)
	require.NoError(t, err)
	require.Len(t, tracks, yandexTrackIDsBatchSize+1)
	require.Len(t, requests, 2)
	assert.Len(t, requests[0], yandexTrackIDsBatchSize)
	assert.Len(t, requests[1], 1)
}
