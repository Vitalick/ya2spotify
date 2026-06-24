package yandexmusic

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const playlistID = "65b2e7a9-b735-e097-9b2c-ec6a89125c71"
const playlistURL = "https://music.yandex.ru/playlists/" + playlistID
const playlistFile = "testplaylists/" + playlistID + ".html"

func TestGetYandexState(t *testing.T) {
	arrays, err := getStateUpdateListFromFile(playlistFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, arrays)
	yandexState, err := getYandexState(arrays)
	assert.NoError(t, err)
	assert.NotEmpty(t, yandexState)
	//if f, err := os.Create(fmt.Sprintf("testplaylists/%s.json", playlistID)); err == nil {
	//	json.NewEncoder(f).Encode(yandexState)
	//	f.Close()
	//}
	playlist, ok := yandexState["playlist"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, playlistID, playlist["uuid"])
	assert.Equal(t, "RESOLVE", playlist["loadingState"])

	items, ok := playlist["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 6)
	for _, item := range items {
		itemObj, ok := item.(map[string]any)
		require.True(t, ok)
		data, ok := itemObj["data"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "music", data["trackType"])
	}

	for i := range 2 {
		var fileObj []stateUpdate
		arrFile, err := os.ReadFile(fmt.Sprintf("testplaylists/%s_%d.json", playlistID, i+1))
		assert.NoError(t, err)
		err = json.Unmarshal(arrFile, &fileObj)
		assert.NoError(t, err)
		assert.Equal(t, arrays[:len(fileObj)], fileObj)
		arrays = arrays[len(fileObj):]
	}
}

func TestGetYandexStateOperations(t *testing.T) {
	state, err := getYandexState([]stateUpdate{
		{Op: "replace", Path: "/playlist/title", Value: "first"},
		{Op: "add", Path: "/playlist/items/0", Value: map[string]any{"id": "1"}},
		{Op: "add", Path: "/playlist/items/-", Value: map[string]any{"id": "2"}},
		{Op: "replace", Path: "/playlist/items/0/data/trackType", Value: "music"},
		{Op: "remove", Path: "/playlist/items/1"},
	})
	require.NoError(t, err)

	playlist, ok := state["playlist"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "first", playlist["title"])

	items, ok := playlist["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 1)

	item, ok := items[0].(map[string]any)
	require.True(t, ok)
	data, ok := item["data"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "music", data["trackType"])
}

func TestDecodeYandexStatePlaylist(t *testing.T) {
	stateFile, err := os.ReadFile("testplaylists/" + playlistID + ".json")
	require.NoError(t, err)

	var state map[string]any
	require.NoError(t, json.Unmarshal(stateFile, &state))

	playlist, err := decodeYandexStatePlaylist(state)
	require.NoError(t, err)
	assert.Equal(t, playlistID, playlist.UUID)
	assert.Equal(t, "Змеиный плейлист", playlist.Meta.Title)
	assert.Equal(t, "music-blog", playlist.Meta.Owner.Login)
	assert.Equal(t, int64(2324), playlist.Meta.Kind)

	tracks := playlist.TrackEntries()
	require.Len(t, tracks, 6)
	assert.Equal(t, "65093274", tracks[0].ID)
	assert.Equal(t, "На кончике хвоста", tracks[0].Title)
	require.Len(t, tracks[0].Artists, 1)
	assert.Equal(t, 9088811, tracks[0].Artists[0].ID)
	assert.Equal(t, "Снейк Синатра", tracks[0].Artists[0].Name)
	require.Len(t, tracks[0].Albums, 1)
	assert.Equal(t, 10509403, tracks[0].Albums[0].ID)

	result := NewPlaylist("", 0)
	require.NoError(t, result.importYandexState(state))
	assert.Equal(t, "music-blog", result.Owner)
	assert.Equal(t, int64(2324), result.PlaylistID)
	assert.Equal(t, "Змеиный плейлист", result.Title)
	require.Len(t, result.Tracks, 6)
	assert.Equal(t, tracks[0], result.Tracks[0])
}
