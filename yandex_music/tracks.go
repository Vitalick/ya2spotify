package yandex_music

import (
	"fmt"
	"strings"
)

type TrackArtist struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func (a *TrackArtist) String() string {
	return a.Name
}

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

func (t *SingleTrack) String() string {
	outString := fmt.Sprintf("%s - %s", strings.Join(ArtistsToStrings(t.Artists), ", "), t.Title)
	if len(t.Version) > 0 {
		outString += fmt.Sprintf("%s (%s)", outString, t.Version)
	}
	return outString
}
