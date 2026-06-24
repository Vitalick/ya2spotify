package yandexmusic

import (
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
	err = getStatePatchScriptNodes(htmlFile)
	assert.NoError(t, err)
}
