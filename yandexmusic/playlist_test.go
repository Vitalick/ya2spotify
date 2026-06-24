package yandexmusic

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPlaylist(t *testing.T) {
	playlist := NewPlaylist("music-blog", 103372440)
	err := playlist.GetTracks()
	assert.NoError(t, err)
	assert.NotZero(t, len(playlist.Tracks))
}
