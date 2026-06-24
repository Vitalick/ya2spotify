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
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	yandexTrackIDsBatchSize = 50
	yandexTracksWorkers     = 4
	yandexRequestDelay      = 300 * time.Millisecond
	yandexMaxRetries        = 3
)

var (
	yandexMusicAPIBaseURL = "https://api.music.yandex.ru"
	yandexHTTPClient      = http.DefaultClient
)

type yandexAPIPlaylistResponse struct {
	Result yandexAPIPlaylist `json:"result"`
	Error  *yandexAPIError   `json:"error"`
	raw    []byte
}

type yandexAPIPlaylist struct {
	PlaylistUUID string                `json:"playlistUuid"`
	Owner        yandexAPIOwner        `json:"owner"`
	Kind         int64                 `json:"kind"`
	Title        string                `json:"title"`
	Description  string                `json:"description"`
	TrackIDsList []yandexAPITrackID    `json:"trackIds"`
	Tracks       []yandexAPITrackEntry `json:"tracks"`
}

type yandexAPIOwner struct {
	Login string `json:"login"`
	UID   int64  `json:"uid"`
}

type yandexAPITrackEntry struct {
	ID yandexAPITrackID `json:"id"`
}

type yandexAPITrackID string

type yandexAPIError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
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

	p.sendProgress(TrackProgress{Total: len(trackIDs)})

	tracks, err := fetchYandexTracks(trackIDs, p.PlaylistURL(), p.Progress)
	if err != nil {
		return err
	}
	p.Tracks = tracks
	p.imported = true
	return nil
}

func (p *Playlist) sendProgress(progress TrackProgress) {
	if p.Progress == nil {
		return
	}
	p.Progress <- progress
}

func (p *Playlist) fetchAPIPlaylist() (yandexAPIPlaylist, error) {
	playlistID := p.apiPlaylistID()
	if playlistID == "" {
		return yandexAPIPlaylist{}, errors.New("playlist api id is empty")
	}

	req, err := http.NewRequest(http.MethodGet, yandexMusicAPIBaseURL+p.apiPlaylistPath(), nil)
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
	if response.Error != nil {
		return yandexAPIPlaylist{}, fmt.Errorf(
			"yandex api error %s: %s",
			response.Error.Name,
			response.Error.Message,
		)
	}
	if response.Result.Title == "" {
		return yandexAPIPlaylist{}, fmt.Errorf(
			"playlist title is empty for %s; response keys: %s",
			req.URL.Path,
			jsonObjectKeys(response.raw),
		)
	}
	return response.Result, nil
}

func (r *yandexAPIPlaylistResponse) UnmarshalJSON(data []byte) error {
	r.raw = append(r.raw[:0], data...)

	var wrapper struct {
		Result json.RawMessage `json:"result"`
		Error  *yandexAPIError `json:"error"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return err
	}
	r.Error = wrapper.Error

	if len(wrapper.Result) > 0 && string(wrapper.Result) != "null" {
		playlist, err := decodeYandexAPIPlaylist(wrapper.Result)
		if err != nil {
			return err
		}
		r.Result = playlist
		return nil
	}

	playlist, err := decodeYandexAPIPlaylist(data)
	if err != nil {
		return err
	}
	r.Result = playlist
	return nil
}

func decodeYandexAPIPlaylist(data []byte) (yandexAPIPlaylist, error) {
	var playlist yandexAPIPlaylist
	if err := json.Unmarshal(data, &playlist); err != nil {
		return yandexAPIPlaylist{}, err
	}
	if playlist.Title != "" {
		return playlist, nil
	}

	var wrapper struct {
		Playlist yandexAPIPlaylist `json:"playlist"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return yandexAPIPlaylist{}, err
	}
	if wrapper.Playlist.Title != "" {
		return wrapper.Playlist, nil
	}

	return playlist, nil
}

func jsonObjectKeys(data []byte) []string {
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil
	}

	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

func (p *Playlist) apiPlaylistPath() string {
	if p.UUID != "" {
		return "/playlist/" + p.apiPlaylistID()
	}
	if p.Owner != "" && p.PlaylistID != 0 {
		return fmt.Sprintf("/users/%s/playlists/%d", p.Owner, p.PlaylistID)
	}
	return "/playlist/" + p.apiPlaylistID()
}

func (p *Playlist) importAPIPlaylist(playlist yandexAPIPlaylist) {
	p.UUID = playlist.PlaylistUUID
	p.Owner = playlist.Owner.Login
	p.PlaylistID = playlist.Kind
	p.Title = playlist.Title
	p.Description = playlist.Description
}

func (p *yandexAPIPlaylist) TrackIDs() []string {
	if len(p.TrackIDsList) > 0 {
		trackIDs := make([]string, 0, len(p.TrackIDsList))
		for _, trackID := range p.TrackIDsList {
			if trackID == "" {
				continue
			}
			trackIDs = append(trackIDs, string(trackID))
		}
		return trackIDs
	}

	trackIDs := make([]string, 0, len(p.Tracks))
	for _, track := range p.Tracks {
		if track.ID == "" {
			continue
		}
		trackIDs = append(trackIDs, string(track.ID))
	}
	return trackIDs
}

func (id *yandexAPITrackID) UnmarshalJSON(data []byte) error {
	var stringID string
	if err := json.Unmarshal(data, &stringID); err == nil {
		*id = yandexAPITrackID(stringID)
		return nil
	}

	var intID int64
	if err := json.Unmarshal(data, &intID); err == nil {
		*id = yandexAPITrackID(strconv.FormatInt(intID, 10))
		return nil
	}

	return fmt.Errorf("unexpected track id value %s", string(data))
}

type yandexTrackBatchJob struct {
	Index    int
	TrackIDs []string
}

type yandexTrackBatchResult struct {
	Index    int
	TrackIDs []string
	Tracks   []SingleTrack
	Err      error
}

func fetchYandexTracks(trackIDs []string, retpath string, progress chan<- TrackProgress) ([]SingleTrack, error) {
	jobs := make([]yandexTrackBatchJob, 0, (len(trackIDs)+yandexTrackIDsBatchSize-1)/yandexTrackIDsBatchSize)
	for start := 0; start < len(trackIDs); start += yandexTrackIDsBatchSize {
		end := min(start+yandexTrackIDsBatchSize, len(trackIDs))
		jobs = append(jobs, yandexTrackBatchJob{
			Index:    len(jobs),
			TrackIDs: trackIDs[start:end],
		})
	}

	jobsChan := make(chan yandexTrackBatchJob)
	resultsChan := make(chan yandexTrackBatchResult)
	ticks := time.Tick(yandexRequestDelay)
	var wg sync.WaitGroup
	for range min(yandexTracksWorkers, len(jobs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobsChan {
				<-ticks
				tracks, err := fetchYandexTracksBatch(job.TrackIDs, retpath)
				resultsChan <- yandexTrackBatchResult{
					Index:    job.Index,
					TrackIDs: job.TrackIDs,
					Tracks:   tracks,
					Err:      err,
				}
			}
		}()
	}

	go func() {
		for _, job := range jobs {
			jobsChan <- job
		}
		close(jobsChan)
		wg.Wait()
		close(resultsChan)
	}()

	done := 0
	failed := 0
	var errList []error
	results := make([][]SingleTrack, len(jobs))
	for result := range resultsChan {
		if result.Err != nil {
			failed += len(result.TrackIDs)
			errList = append(errList, result.Err)
		} else {
			done += len(result.Tracks)
			failed += max(0, len(result.TrackIDs)-len(result.Tracks))
			results[result.Index] = result.Tracks
		}
		sendYandexTrackProgress(progress, TrackProgress{Total: len(trackIDs), Done: done, Failed: failed})
	}
	if len(errList) > 0 {
		return nil, errors.Join(errList...)
	}

	tracks := make([]SingleTrack, 0, len(trackIDs))
	for _, batch := range results {
		tracks = append(tracks, batch...)
	}
	return tracks, nil
}

func sendYandexTrackProgress(progress chan<- TrackProgress, value TrackProgress) {
	if progress == nil {
		return
	}
	progress <- value
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
