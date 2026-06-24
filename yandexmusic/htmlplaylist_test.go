package yandexmusic

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

const playlistID = "65b2e7a9-b735-e097-9b2c-ec6a89125c71"
const playlistURL = "https://music.yandex.ru/playlists/" + playlistID
const playlistFile = "testplaylists/" + playlistID + ".html"

func TestGetStatePatchScriptNodes(t *testing.T) {
	htmlFile, err := os.Open(playlistFile)
	assert.NoError(t, err)
	defer htmlFile.Close()
	arrays, err := getStatePatchScriptNodes(htmlFile)
	assert.NoError(t, err)
	assert.NotEmpty(t, arrays)
	for i := range 2 {
		var fileObj []stateUpdate
		arrFile, err := os.ReadFile(fmt.Sprintf("testplaylists/%d.json", i+1))
		assert.NoError(t, err)
		err = json.Unmarshal(arrFile, &fileObj)
		assert.NoError(t, err)
		assert.Equal(t, arrays[:len(fileObj)], fileObj)
		arrays = arrays[len(fileObj):]
	}
}
