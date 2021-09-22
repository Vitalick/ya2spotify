package yandex_music

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

type yandexPlaylistData struct {
	Playlist struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		TrackIds    []string `json:"trackIds"`
	} `json:"playlist"`
}

type postDataTrackEntries struct {
	Entries        string `json:"entries"`
	Language       string `json:"lang"`
	ExternalDomain string `json:"external-domain"`
	OverEmbedded   bool   `json:"overembed"`
}

func (y *yandexPlaylistData) formattedTrackIds() string {
	return strings.Join(y.Playlist.TrackIds, ",")
}

func (y *yandexPlaylistData) dataTrackEntries() *postDataTrackEntries {
	return &postDataTrackEntries{
		y.formattedTrackIds(),
		"ru",
		"music.yandex.ru",
		false,
	}
}

func (y *yandexPlaylistData) TrackEntries() ([]SingleTrackEntries, error) {
	json_data, err := json.Marshal(*y.dataTrackEntries())

	if err != nil {
		return nil, err
	}
	resp, err := http.Post(trackEntriesApi(), "application/json", bytes.NewBuffer(json_data))
	if err != nil {
		return nil, err
	}

	var res []SingleTrackEntries

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
