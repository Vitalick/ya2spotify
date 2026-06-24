package yandexmusic

import "fmt"

const (
	yandexAPIURL          = "https://music.yandex.ru/handlers/"
	yandexAPITrackEntries = "track-entries.jsx"
	yandexAPIPlaylist     = "playlist.jsx"
)

// trackEntriesAPI возвращает endpoint для получения подробной информации по трекам.
//
// Returns:
//   - string: абсолютный URL endpoint'а track-entries.jsx.
func trackEntriesAPI() string {
	return fmt.Sprintf("%s%s", yandexAPIURL, yandexAPITrackEntries)
}

// playlistAPI возвращает endpoint для получения метаданных плейлиста.
//
// Returns:
//   - string: абсолютный URL endpoint'а playlist.jsx.
func playlistAPI() string {
	return fmt.Sprintf("%s%s", yandexAPIURL, yandexAPIPlaylist)
}
