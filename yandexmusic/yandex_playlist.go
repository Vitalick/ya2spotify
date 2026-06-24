package yandexmusic

import (
	"errors"

	"github.com/go-viper/mapstructure/v2"
)

type yandexStateData struct {
	Playlist yandexStatePlaylist `mapstructure:"playlist"`
}

type yandexStatePlaylist struct {
	UUID  string                  `mapstructure:"uuid"`
	Meta  yandexStatePlaylistMeta `mapstructure:"meta"`
	Items []yandexStateTrackItem  `mapstructure:"items"`
}

type yandexStatePlaylistMeta struct {
	Title       string           `mapstructure:"title"`
	Description string           `mapstructure:"description"`
	Kind        int64            `mapstructure:"kind"`
	Owner       yandexStateOwner `mapstructure:"owner"`
}

type yandexStateOwner struct {
	Login string `mapstructure:"login"`
	UID   int64  `mapstructure:"uid"`
}

type yandexStateTrackItem struct {
	ID   string       `mapstructure:"id"`
	Data *SingleTrack `mapstructure:"data"`
}

func decodeYandexStatePlaylist(state map[string]any) (yandexStatePlaylist, error) {
	var data yandexStateData
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           &data,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
	})
	if err != nil {
		return yandexStatePlaylist{}, err
	}
	if err := decoder.Decode(state); err != nil {
		return yandexStatePlaylist{}, err
	}
	if data.Playlist.Meta.Title == "" {
		return yandexStatePlaylist{}, errors.New("playlist meta title is empty")
	}
	return data.Playlist, nil
}

func (p *Playlist) importYandexState(state map[string]any) error {
	playlist, err := decodeYandexStatePlaylist(state)
	if err != nil {
		return err
	}

	p.UUID = playlist.UUID
	p.Owner = playlist.Meta.Owner.Login
	p.PlaylistID = playlist.Meta.Kind
	p.Title = playlist.Meta.Title
	p.Description = playlist.Meta.Description
	p.Tracks = playlist.TrackEntries()
	return nil
}

func (p *yandexStatePlaylist) TrackEntries() []SingleTrack {
	tracks := make([]SingleTrack, 0, len(p.Items))
	for _, item := range p.Items {
		if item.Data == nil {
			continue
		}
		tracks = append(tracks, *item.Data)
	}
	return tracks
}
