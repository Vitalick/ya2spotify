package yandexmusic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

type Playlist struct {
	Owner          string        `json:"owner"`
	PlaylistID     int64         `json:"playlist_id"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Tracks         []SingleTrack `json:"tracks"`
	yandexPlaylist yandexPlaylistDataOuter
}

// NewPlaylist создает объект плейлиста Яндекс Музыки по владельцу и идентификатору.
//
// Parameters:
//   - owner: логин или идентификатор владельца плейлиста.
//   - playlistID: числовой идентификатор плейлиста.
//
// Returns:
//   - *Playlist: плейлист без загруженных метаданных и треков.
func NewPlaylist(owner string, playlistID int64) *Playlist {
	return &Playlist{
		owner,
		playlistID,
		"",
		"",
		[]SingleTrack{},
		yandexPlaylistDataOuter{},
	}
}

// NewPlaylistFromLink разбирает ссылку Яндекс Музыки и создает объект плейлиста.
//
// Parameters:
//   - playlistLink: ссылка на плейлист в формате с сегментом "/playlists/<id>".
//
// Returns:
//   - *Playlist: плейлист с владельцем и идентификатором из ссылки.
//   - error: ошибка парсинга идентификатора или ошибка невалидной ссылки.
//
// Cases:
//   - пустая ссылка возвращает nil, nil.
//   - ссылка без сегмента playlists возвращает ошибку "invalid link".
func NewPlaylistFromLink(playlistLink string) (*Playlist, error) {
	var owner string
	var playlistID int64
	if len(playlistLink) > 0 {
		re := regexp.MustCompile("/+")
		splitURL := re.Split(playlistLink, -1)
		playlistsIndex := len(splitURL)
		for i, urlPart := range splitURL {
			if urlPart == "playlists" {
				playlistsIndex = i
				break
			}
		}
		if playlistsIndex < len(splitURL)-1 {
			owner = splitURL[playlistsIndex-1]
			var err error
			playlistID, err = strconv.ParseInt(splitURL[playlistsIndex+1], 10, 64)
			if err != nil {
				return nil, err
			}
			return NewPlaylist(owner, playlistID), nil
		}
		return nil, errors.New("invalid link")
	}
	return nil, nil
}

// PlaylistURL собирает URL запроса метаданных плейлиста к Яндекс Музыке.
//
// Returns:
//   - string: URL endpoint'а playlist.jsx с query-параметрами текущего плейлиста.
func (p *Playlist) PlaylistURL() string {
	values := url.Values{
		"owner":           {p.Owner},
		"kinds":           {strconv.FormatInt(p.PlaylistID, 10)},
		"light":           {"true"},
		"madeFor":         {""},
		"lang":            {"ru"},
		"external-domain": {"music.yandex.ru"},
		"overembed":       {"false"},
	}

	urlArgs := values.Encode()
	if len(urlArgs) > 0 {
		return fmt.Sprintf("%s?%s", playlistAPI(), urlArgs)
	}
	return playlistAPI()
}

// GetPlaylistInfo загружает метаданные плейлиста из Яндекс Музыки и обновляет Playlist.
//
// Returns:
//   - error: ошибка HTTP-запроса, декодирования JSON или закрытия response body.
func (p *Playlist) GetPlaylistInfo() error {
	resp, err := http.Get(p.PlaylistURL())
	if err != nil {
		return err
	}
	bodyStr, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(bodyStr, &p.yandexPlaylist); err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return err
	}
	p.yandexPlaylist.imported = true
	p.Title = p.yandexPlaylist.Playlist.Title
	p.Description = p.yandexPlaylist.Playlist.Description
	return nil
}

// getTracks загружает список треков и при необходимости предварительно обновляет метаданные плейлиста.
//
// Parameters:
//   - bypassUpdatePlaylist: принудительно обновлять метаданные плейлиста перед загрузкой треков.
//
// Returns:
//   - error: ошибка получения метаданных или списка треков.
func (p *Playlist) getTracks(bypassUpdatePlaylist bool) error {
	if !p.yandexPlaylist.imported || bypassUpdatePlaylist {
		if err := p.GetPlaylistInfo(); err != nil {
			return err
		}
	}
	tracks, err := p.yandexPlaylist.TrackEntries()
	if tracks != nil {
		p.Tracks = tracks
	}
	return err
}

// GetTracks загружает треки плейлиста, используя уже загруженные метаданные при их наличии.
//
// Returns:
//   - error: ошибка загрузки метаданных или треков.
func (p *Playlist) GetTracks() error {
	return p.getTracks(false)
}

// GetAllBypass принудительно обновляет метаданные плейлиста и список треков.
//
// Returns:
//   - error: ошибка загрузки метаданных или треков.
func (p *Playlist) GetAllBypass() error {
	return p.getTracks(true)
}
