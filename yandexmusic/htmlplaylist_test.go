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
