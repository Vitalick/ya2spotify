package yandexmusic

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Playlist struct {
	Owner       string        `json:"owner"`
	PlaylistID  int64         `json:"playlist_id"`
	UUID        string        `json:"uuid"`
	SourceURL   string        `json:"source_url"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Tracks      []SingleTrack `json:"tracks"`
	Progress    chan<- TrackProgress
	imported    bool
}

type TrackProgress struct {
	Total  int
	Done   int
	Failed int
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
		Owner:      owner,
		PlaylistID: playlistID,
		SourceURL:  yandexMusicPlaylistPageURL(owner, playlistID),
		Tracks:     []SingleTrack{},
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
	if playlistLink == "" {
		return nil, nil
	}

	pageURL, err := normalizeYandexPlaylistURL(playlistLink)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile("/+")
	splitPath := re.Split(strings.Trim(pageURL.Path, "/"), -1)
	playlistsIndex := len(splitPath)
	for i, urlPart := range splitPath {
		if urlPart == "playlists" {
			playlistsIndex = i
			break
		}
	}
	if playlistsIndex >= len(splitPath)-1 {
		return nil, errors.New("invalid link")
	}

	playlist := &Playlist{
		SourceURL: pageURL.String(),
		Tracks:    []SingleTrack{},
	}

	playlistID := splitPath[playlistsIndex+1]
	if parsedID, err := strconv.ParseInt(playlistID, 10, 64); err == nil {
		playlist.PlaylistID = parsedID
	} else {
		playlist.UUID = playlistID
	}

	if playlistsIndex > 0 {
		playlist.Owner = splitPath[playlistsIndex-1]
		if playlist.Owner == "users" {
			playlist.Owner = ""
		}
	}
	return playlist, nil
}

// PlaylistURL возвращает URL страницы плейлиста Яндекс Музыки.
//
// Returns:
//   - string: URL HTML-страницы плейлиста.
func (p *Playlist) PlaylistURL() string {
	if p.SourceURL != "" {
		return p.SourceURL
	}
	if p.UUID != "" {
		return fmt.Sprintf("https://music.yandex.ru/playlists/%s", p.UUID)
	}
	return yandexMusicPlaylistPageURL(p.Owner, p.PlaylistID)
}

// GetPlaylistInfo загружает метаданные и треки плейлиста через API Яндекс Музыки.
//
// Returns: error: ошибка HTTP-запроса или декодирования API-ответа.
func (p *Playlist) GetPlaylistInfo() error {
	return p.loadFromYandexAPI()
}

// getTracks загружает список треков и при необходимости предварительно обновляет метаданные плейлиста.
//
// Parameters:
//   - bypassUpdatePlaylist: принудительно обновлять метаданные плейлиста перед загрузкой треков.
//
// Returns:
//   - error: ошибка получения метаданных или списка треков.
func (p *Playlist) getTracks(bypassUpdatePlaylist bool) error {
	if p.imported && !bypassUpdatePlaylist {
		return nil
	}
	return p.GetPlaylistInfo()
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

func normalizeYandexPlaylistURL(playlistLink string) (*url.URL, error) {
	playlistLink = strings.TrimSpace(playlistLink)
	if playlistLink == "" {
		return nil, errors.New("invalid link")
	}
	if strings.HasPrefix(playlistLink, "/") {
		playlistLink = "https://music.yandex.ru" + playlistLink
	}
	if strings.HasPrefix(playlistLink, "playlists/") || strings.HasPrefix(playlistLink, "users/") {
		playlistLink = "https://music.yandex.ru/" + playlistLink
	}
	if !strings.Contains(playlistLink, "://") {
		playlistLink = "https://" + strings.TrimLeft(playlistLink, "/")
	}
	parsedURL, err := url.Parse(playlistLink)
	if err != nil {
		return nil, err
	}
	if parsedURL.Host == "" {
		return nil, errors.New("invalid link")
	}
	if parsedURL.Scheme == "" {
		parsedURL.Scheme = "https"
	}
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	return parsedURL, nil
}

func yandexMusicPlaylistPageURL(owner string, playlistID int64) string {
	if owner == "" || playlistID == 0 {
		return "https://music.yandex.ru"
	}
	return fmt.Sprintf("https://music.yandex.ru/users/%s/playlists/%d", owner, playlistID)
}
