package yandexmusic

import (
	"fmt"
	"strings"
)

type TrackArtist struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// String возвращает имя исполнителя.
//
// Returns:
//   - string: имя исполнителя из данных Яндекс Музыки.
func (a *TrackArtist) String() string {
	return a.Name
}

// ArtistsToStrings преобразует список исполнителей в список имен.
//
// Parameters:
//   - a: исполнители из трека или альбома.
//
// Returns:
//   - []string: имена исполнителей в исходном порядке.
func ArtistsToStrings(a []TrackArtist) []string {
	var stringArtists []string
	for _, artist := range a {
		stringArtists = append(stringArtists, artist.String())
	}
	return stringArtists
}

type TrackAlbum struct {
	ID      int           `json:"id"`
	Title   string        `json:"title"`
	Artists []TrackArtist `json:"artists"`
}

// String форматирует альбом как название со списком исполнителей.
//
// Returns:
//   - string: строка вида "<альбом> [<исполнители>]".
func (a *TrackAlbum) String() string {
	return fmt.Sprintf("%s [%s]", a.Title, strings.Join(ArtistsToStrings(a.Artists), ", "))
}

type SingleTrack struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Version string        `json:"version"`
	Artists []TrackArtist `json:"artists"`
	Albums  []TrackAlbum  `json:"albums"`
}

// String форматирует трек как строку для HTML-страниц приложения.
//
// Returns:
//   - string: строка с исполнителями, названием и версией трека при ее наличии.
func (t *SingleTrack) String() string {
	outString := fmt.Sprintf("%s - %s", strings.Join(ArtistsToStrings(t.Artists), ", "), t.Title)
	if len(t.Version) > 0 {
		outString += fmt.Sprintf("%s (%s)", outString, t.Version)
	}
	return outString
}
