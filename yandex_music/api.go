package yandex_music

import "fmt"

const (
	yandexApiUrl          = "https://music.yandex.ru/handlers/"
	yandexApiTrackEntries = "track-entries.jsx"
	yandexApiPlaylist     = "playlist.jsx"
)

func trackEntriesApi() string {
	return fmt.Sprintf("%s%s", yandexApiUrl, yandexApiTrackEntries)
}

func playlistApi() string {
	return fmt.Sprintf("%s%s", yandexApiUrl, yandexApiPlaylist)
}
