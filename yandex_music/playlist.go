package yandex_music

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type Playlist struct {
	Owner          string               `json:"owner"`
	PlaylistId     int64                `json:"playlist_id"`
	Title          string               `json:"title"`
	Description    string               `json:"description"`
	Tracks         []SingleTrackEntries `json:"tracks"`
	yandexPlaylist *yandexPlaylistData
}

func NewPlaylist(owner string, playlistId int64) *Playlist {
	return &Playlist{
		owner,
		playlistId,
		"",
		"",
		[]SingleTrackEntries{},
		&yandexPlaylistData{},
	}
}

func (p *Playlist) PlaylistUrl() string {
	values := url.Values{
		"owner":           {p.Owner},
		"kinds":           {strconv.FormatInt(p.PlaylistId, 10)},
		"light":           {"true"},
		"madeFor":         {""},
		"lang":            {"ru"},
		"external-domain": {"music.yandex.ru"},
		"overembed":       {"false"},
	}

	urlArgs := values.Encode()
	if len(urlArgs) > 0 {
		return fmt.Sprintf("%s?%s", playlistApi(), urlArgs)
	}
	return playlistApi()
}

func (p *Playlist) GetPlaylistInfo() error {
	resp, err := http.Get(p.PlaylistUrl())
	if err != nil {
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(p.yandexPlaylist); err != nil {
		return err
	}
	if err := resp.Body.Close(); err != nil {
		return err
	}
	p.yandexPlaylist.imported = true
	return nil
}

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

func (p *Playlist) GetTracks() error {
	return p.getTracks(false)
}

func (p *Playlist) GetAllBypass() error {
	return p.getTracks(true)
}
