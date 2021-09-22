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

func (p *Playlist) playlistInfo() error {
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
	return nil
}

func (p *Playlist) DataTrackEntries() error {
	if err := p.playlistInfo(); err != nil {
		return err
	}
	tracks, err := p.yandexPlaylist.TrackEntries()
	if tracks != nil {
		p.Tracks = tracks
	}
	return err
}
