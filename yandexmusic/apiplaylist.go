// Package yandexmusic загружает плейлисты и треки из Яндекс Музыки.
package yandexmusic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	yandexTrackIDsBatchSize = 50
	yandexRequestDelay      = 300 * time.Millisecond
	yandexMaxRetries        = 3
)

var (
	yandexMusicAPIBaseURL = "https://api.music.yandex.ru"
	yandexHTTPClient      = http.DefaultClient
)

type yandexAPIPlaylistResponse struct {
	Result yandexAPIPlaylist `json:"result"`
}

type yandexAPIPlaylist struct {
	PlaylistUUID string                `json:"playlistUuid"`
	Owner        yandexAPIOwner        `json:"owner"`
	Kind         int64                 `json:"kind"`
	Title        string                `json:"title"`
	Description  string                `json:"description"`
	Tracks       []yandexAPITrackEntry `json:"tracks"`
}

type yandexAPIOwner struct {
	Login string `json:"login"`
	UID   int64  `json:"uid"`
}

type yandexAPITrackEntry struct {
	ID int64 `json:"id"`
}

func (p *Playlist) loadFromYandexAPI() error {
	playlist, err := p.fetchAPIPlaylist()
	if err != nil {
		return err
	}

	p.importAPIPlaylist(playlist)

	trackIDs := playlist.TrackIDs()
	if len(trackIDs) == 0 {
		p.Tracks = []SingleTrack{}
		p.imported = true
		return nil
	}

	tracks, err := fetchYandexTracks(trackIDs, p.PlaylistURL())
	if err != nil {
		return err
	}
	p.Tracks = tracks
	p.imported = true
	return nil
}

func (p *Playlist) fetchAPIPlaylist() (yandexAPIPlaylist, error) {
	playlistID := p.apiPlaylistID()
	if playlistID == "" {
		return yandexAPIPlaylist{}, errors.New("playlist api id is empty")
	}

	req, err := http.NewRequest(http.MethodGet, yandexMusicAPIBaseURL+"/playlist/"+playlistID, nil)
	if err != nil {
		return yandexAPIPlaylist{}, err
	}
	query := req.URL.Query()
	query.Set("resumeStream", "false")
	query.Set("richTracks", "false")
	req.URL.RawQuery = query.Encode()
	setYandexAPIHeaders(req, p.PlaylistURL())

	var response yandexAPIPlaylistResponse
	if err := doYandexJSONRequest(req, &response); err != nil {
		return yandexAPIPlaylist{}, err
	}
	if response.Result.Title == "" {
		return yandexAPIPlaylist{}, errors.New("playlist title is empty")
	}
	return response.Result, nil
}

func (p *Playlist) apiPlaylistID() string {
	if p.UUID != "" {
		return p.UUID
	}
	if p.PlaylistID != 0 {
		return strconv.FormatInt(p.PlaylistID, 10)
	}
	return ""
}

func (p *Playlist) importAPIPlaylist(playlist yandexAPIPlaylist) {
	p.UUID = playlist.PlaylistUUID
	p.Owner = playlist.Owner.Login
	p.PlaylistID = playlist.Kind
	p.Title = playlist.Title
	p.Description = playlist.Description
}

func (p *yandexAPIPlaylist) TrackIDs() []string {
	trackIDs := make([]string, 0, len(p.Tracks))
	for _, track := range p.Tracks {
		if track.ID == 0 {
			continue
		}
		trackIDs = append(trackIDs, strconv.FormatInt(track.ID, 10))
	}
	return trackIDs
}

func fetchYandexTracks(trackIDs []string, retpath string) ([]SingleTrack, error) {
	tracks := make([]SingleTrack, 0, len(trackIDs))
	for start := 0; start < len(trackIDs); start += yandexTrackIDsBatchSize {
		if start > 0 {
			time.Sleep(yandexRequestDelay)
		}

		end := min(start+yandexTrackIDsBatchSize, len(trackIDs))
		batch, err := fetchYandexTracksBatch(trackIDs[start:end], retpath)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, batch...)
	}
	return tracks, nil
}

func fetchYandexTracksBatch(trackIDs []string, retpath string) ([]SingleTrack, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, trackID := range trackIDs {
		if err := writer.WriteField("trackIds", trackID); err != nil {
			return nil, err
		}
	}
	if err := writer.WriteField("removeDuplicates", "false"); err != nil {
		return nil, err
	}
	if err := writer.WriteField("withProgress", "true"); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, yandexMusicAPIBaseURL+"/tracks", &body)
	if err != nil {
		return nil, err
	}
	setYandexAPIHeaders(req, retpath)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	var tracks []SingleTrack
	if err := doYandexJSONRequest(req, &tracks); err != nil {
		return nil, err
	}
	return tracks, nil
}

func setYandexAPIHeaders(req *http.Request, retpath string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "ru")
	req.Header.Set("Origin", "https://music.yandex.ru")
	req.Header.Set("Referer", "https://music.yandex.ru/")
	req.Header.Set(
		"User-Agent",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36",
	)
	req.Header.Set("X-Request-Id", uuid.NewString())
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-Retpath-Y", retpath)
	req.Header.Set("X-Yandex-Music-Client", "YandexMusicWebNext/1.0.0")
	req.Header.Set("X-Yandex-Music-Without-Invocation-Info", "1")
}

func doYandexJSONRequest(req *http.Request, dst any) error {
	var lastErr error
	var retryAfter string
	for attempt := range yandexMaxRetries + 1 {
		if attempt > 0 {
			waitBeforeYandexRetry(retryAfter)
			if req.GetBody != nil {
				body, err := req.GetBody()
				if err != nil {
					return err
				}
				req.Body = body
			}
		}

		resp, err := yandexHTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter = resp.Header.Get("Retry-After")
			lastErr = fmt.Errorf("yandex api rate limited %s", req.URL.Path)
			_ = resp.Body.Close()
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			return fmt.Errorf(
				"unexpected status code %d for %s: %s",
				resp.StatusCode,
				req.URL.String(),
				string(responseBody),
			)
		}

		err = json.NewDecoder(resp.Body).Decode(dst)
		_ = resp.Body.Close()
		return err
	}
	return lastErr
}

func waitBeforeYandexRetry(retryAfter string) {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			time.Sleep(time.Duration(seconds) * time.Second)
			return
		}
		if retryAt, err := http.ParseTime(retryAfter); err == nil {
			if wait := time.Until(retryAt); wait > 0 {
				time.Sleep(wait)
			}
			return
		}
	}
	time.Sleep(yandexRequestDelay)
}
