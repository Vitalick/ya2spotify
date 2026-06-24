package yandexmusic

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
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
