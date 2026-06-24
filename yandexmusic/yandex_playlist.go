package yandexmusic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-viper/mapstructure/v2"
)

type yandexPlaylistData struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	TrackIDs    []string `json:"trackIds"`
}

type yandexPlaylistDataOuter struct {
	Playlist yandexPlaylistData `json:"playlist"`
	imported bool
}

type postDataTrackEntries struct {
	Entries        string `json:"entries"`
	Language       string `json:"lang"`
	ExternalDomain string `json:"external-domain"`
	OverEmbedded   bool   `json:"overembed"`
}

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
		return yandexStatePlaylist{}, fmt.Errorf("playlist meta title is empty")
	}
	return data.Playlist, nil
}

func (p *Playlist) importYandexState(state map[string]any) error {
	playlist, err := decodeYandexStatePlaylist(state)
	if err != nil {
		return err
	}

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

// formattedTrackIDs объединяет идентификаторы треков плейлиста для запроса track-entries.
//
// Returns:
//   - string: идентификаторы треков через запятую.
func (y *yandexPlaylistDataOuter) formattedTrackIDs() string {
	return strings.Join(y.Playlist.TrackIDs, ",")
}

// dataTrackEntries собирает тело POST-запроса для получения подробной информации по трекам.
//
// Returns:
//   - *postDataTrackEntries: параметры запроса track-entries.jsx.
func (y *yandexPlaylistDataOuter) dataTrackEntries() *postDataTrackEntries {
	return &postDataTrackEntries{
		y.formattedTrackIDs(),
		"ru",
		"music.yandex.ru",
		false,
	}
}

// TrackEntries получает подробные данные треков плейлиста из Яндекс Музыки.
//
// Returns:
//   - []SingleTrack: список треков в порядке, возвращенном API.
//   - error: ошибка сериализации запроса, HTTP-запроса или декодирования JSON-ответа.
func (y *yandexPlaylistDataOuter) TrackEntries() ([]SingleTrack, error) {
	jsonData, err := json.Marshal(*y.dataTrackEntries())

	if err != nil {
		return nil, err
	}
	resp, err := http.Post(trackEntriesAPI(), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	var res []SingleTrack

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	//body, err := ioutil.ReadAll(resp.Body)
	//
	//if err != nil {
	//	log.Fatal(err)
	//}

	//fmt.Println(string(body))

	return res, nil
}
