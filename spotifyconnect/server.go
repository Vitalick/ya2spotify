package spotifyconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"

	"ya2spotify/yandexmusic"
)

type contextKey string

const requestIDKey contextKey = "requestID"

const (
	spotifyMaxConcurrentRequests = 4
	spotifyRequestInterval       = 250 * time.Millisecond
	spotifyMaxSearchRetries      = 3
	spotifyLibraryBatchSize      = 50
)

type yandexSpotifyTrackIDMap map[string]spotify.FullTrack

type server struct {
	router          *http.ServeMux
	handler         http.Handler
	logger          *logrus.Logger
	auth            *spotifyauth.Authenticator
	state           string
	spotifyClient   *spotify.Client
	currentUser     *spotify.PrivateUser
	savedPlaylist   *yandexmusic.Playlist
	yandexSpotify   yandexSpotifyTrackIDMap
	yandexImporting bool
	yandexImportErr error
	mMap            *sync.Mutex
	mCounter        *sync.Mutex
	mClient         *sync.Mutex
	mYandex         *sync.Mutex
	spotifySlots    chan struct{}
	spotifyTicks    <-chan time.Time
	maxThreads      int
	threadsFinish   []bool
	nowSearchTrack  int
}

// newServer создает состояние веб-сервера и регистрирует маршруты приложения.
//
// Parameters:
//   - auth: Spotify OAuth authenticator для логина и получения клиента.
//   - state: OAuth state, который должен вернуться в callback.
//
// Returns:
//   - *server: настроенный сервер с роутером, mutex'ами и количеством worker'ов по CPU.
func newServer(auth *spotifyauth.Authenticator, state string) *server {
	s := &server{
		router:        http.NewServeMux(),
		logger:        logrus.New(),
		auth:          auth,
		state:         state,
		spotifyClient: &spotify.Client{},
		currentUser:   &spotify.PrivateUser{},
		savedPlaylist: nil,
		yandexSpotify: make(yandexSpotifyTrackIDMap),
		mMap:          &sync.Mutex{},
		mCounter:      &sync.Mutex{},
		mClient:       &sync.Mutex{},
		mYandex:       &sync.Mutex{},
		spotifySlots:  make(chan struct{}, spotifyMaxConcurrentRequests),
		spotifyTicks:  time.Tick(spotifyRequestInterval),
		maxThreads:    runtime.NumCPU(),
	}
	for i := 0; i < s.maxThreads; i++ {
		s.threadsFinish = append(s.threadsFinish, true)
	}
	s.configureRouter()

	return s
}

type tracksQuantity struct {
	yandex, foundedSpotify, notFoundedSpotify int
}

// quantityOfTracks считает треки Яндекс Музыки и результаты их поиска в Spotify.
//
// Returns:
//   - *tracksQuantity: количество исходных, найденных и не найденных в Spotify треков.
func (s *server) quantityOfTracks() *tracksQuantity {
	playlist := s.yandexPlaylist()
	if playlist == nil {
		return &tracksQuantity{}
	}

	tracksQuantityYandex := len(playlist.Tracks)
	tracksFoundedSpotify := 0
	tracksNotFoundedSpotify := 0
	for _, track := range playlist.Tracks {
		s.mMap.Lock()
		if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" {
			if spotifyTrack.ID == "nil" {
				tracksNotFoundedSpotify++
			} else {
				tracksFoundedSpotify++
			}
		}
		s.mMap.Unlock()
	}
	return &tracksQuantity{tracksQuantityYandex, tracksFoundedSpotify, tracksNotFoundedSpotify}
}

// foundedTracks собирает найденные в Spotify треки для добавления в новый плейлист.
//
// Returns:
//   - []spotify.FullTrack: найденные треки без записей-маркеров "nil".
func (s *server) foundedTracks() []spotify.FullTrack {
	playlist := s.yandexPlaylist()
	if playlist == nil {
		return nil
	}

	var foundedTracks []spotify.FullTrack
	for _, track := range playlist.Tracks {
		s.mMap.Lock()
		if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" && spotifyTrack.ID != "nil" {
			foundedTracks = append(foundedTracks, spotifyTrack)
		}
		s.mMap.Unlock()
	}
	return foundedTracks
}

func (s *server) yandexPlaylist() *yandexmusic.Playlist {
	s.mYandex.Lock()
	defer s.mYandex.Unlock()
	return s.savedPlaylist
}

func (s *server) yandexImportState() (*yandexmusic.Playlist, bool, error) {
	s.mYandex.Lock()
	defer s.mYandex.Unlock()
	return s.savedPlaylist, s.yandexImporting, s.yandexImportErr
}

func (s *server) startYandexImport(playlist *yandexmusic.Playlist) {
	s.mYandex.Lock()
	s.savedPlaylist = nil
	s.yandexImporting = true
	s.yandexImportErr = nil
	s.mYandex.Unlock()

	s.mMap.Lock()
	s.yandexSpotify = make(yandexSpotifyTrackIDMap)
	s.mMap.Unlock()

	s.mCounter.Lock()
	s.nowSearchTrack = 0
	s.mCounter.Unlock()

	go s.importYandexPlaylist(playlist)
}

func (s *server) importYandexPlaylist(playlist *yandexmusic.Playlist) {
	err := playlist.GetTracks()

	s.mYandex.Lock()
	defer s.mYandex.Unlock()
	s.yandexImporting = false
	if err != nil {
		s.yandexImportErr = err
		return
	}
	s.savedPlaylist = playlist
	s.yandexImportErr = nil
}

// getTrack возвращает следующий трек из импортированного плейлиста для потоковой обработки.
//
// Returns:
//   - *yandexmusic.SingleTrack: следующий трек или nil, если треки закончились.
func (s *server) getTrack() *yandexmusic.SingleTrack {
	playlist := s.yandexPlaylist()
	if playlist == nil {
		return nil
	}

	s.mCounter.Lock()
	defer s.mCounter.Unlock()
	if s.nowSearchTrack >= len(playlist.Tracks) {
		return nil
	}
	s.nowSearchTrack++
	return &playlist.Tracks[s.nowSearchTrack-1]
}

func (s *server) spotifyClientSnapshot() *spotify.Client {
	s.mClient.Lock()
	defer s.mClient.Unlock()
	return s.spotifyClient
}

func (s *server) waitSpotifyRequest(ctx context.Context) error {
	select {
	case <-s.spotifyTicks:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case s.spotifySlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *server) releaseSpotifyRequest() {
	<-s.spotifySlots
}

func (s *server) waitSpotifyRetry(ctx context.Context, err error) bool {
	var spotifyErr spotify.Error
	if !errors.As(err, &spotifyErr) || spotifyErr.Status != http.StatusTooManyRequests {
		return false
	}

	wait := spotifyRequestInterval
	if !spotifyErr.RetryAfter.IsZero() {
		wait = time.Until(spotifyErr.RetryAfter)
		if wait < 0 {
			wait = 0
		}
	}

	select {
	case <-time.After(wait):
		return true
	case <-ctx.Done():
		return false
	}
}

func (s *server) searchSpotify(ctx context.Context, query string) (*spotify.SearchResult, error) {
	var lastErr error
	for attempt := 0; attempt <= spotifyMaxSearchRetries; attempt++ {
		if err := s.waitSpotifyRequest(ctx); err != nil {
			return nil, err
		}
		res, err := s.spotifyClientSnapshot().Search(ctx, query, spotify.SearchTypeTrack, spotify.Limit(10))
		s.releaseSpotifyRequest()
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !s.waitSpotifyRetry(ctx, err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (s *server) hasSpotifySearchResult(trackID string) bool {
	s.mMap.Lock()
	defer s.mMap.Unlock()
	spotifyTrack := s.yandexSpotify[trackID]
	return spotifyTrack.ID != ""
}

// searchTrackInSpotify ищет один трек Яндекс Музыки в Spotify и сохраняет результат в карту соответствий.
//
// Parameters:
//   - yt: трек Яндекс Музыки для поиска.
//
// Cases:
//   - если пользователь Spotify не авторизован, поиск не выполняется.
//   - если трек не найден, в карту записывается marker-трек с ID "nil".
func (s *server) searchTrackInSpotify(yt *yandexmusic.SingleTrack) {
	ctx := context.Background()
	if len(s.currentUser.ID) == 0 {
		return
	}
	if s.hasSpotifySearchResult(yt.ID) {
		return
	}

	var foundTrack spotify.FullTrack
	for _, artist := range yt.Artists {
		searchString := strings.Join([]string{yt.Title, artist.String()}, " ")
		res, err := s.searchSpotify(ctx, searchString)
		if err != nil {
			s.logger.Error(err)
			continue
		}
		if tracks := res.Tracks.Tracks; len(tracks) > 0 {
			foundTrack = tracks[0]
			break
		}
	}
	if len(foundTrack.ID) == 0 {
		foundTrack = spotify.FullTrack{SimpleTrack: spotify.SimpleTrack{ID: "nil"}}
	}
	s.mMap.Lock()
	s.yandexSpotify[yt.ID] = foundTrack
	s.mMap.Unlock()
}

// searchTracksInSpotifyChan обрабатывает треки из канала в отдельном worker'е поиска.
//
// Parameters:
//   - tracksChan: канал треков для поиска.
//   - thread: индекс worker'а в массиве статусов завершения.
func (s *server) searchTracksInSpotifyChan(tracksChan chan yandexmusic.SingleTrack, thread int) {
	for yt := range tracksChan {
		if yt.ID == "" {
			break
		}
		s.searchTrackInSpotify(&yt)
	}
	s.threadsFinish[thread] = true
}

// searchTracksInSpotify запускает параллельный поиск всех импортированных треков в Spotify.
//
// Cases:
//   - если пользователь не авторизован, поиск не запускается.
//   - если предыдущий поиск еще идет, новый запуск игнорируется.
func (s *server) searchTracksInSpotify() {
	if len(s.currentUser.ID) == 0 {
		return
	}
	playlist := s.yandexPlaylist()
	if playlist == nil {
		return
	}
	for _, thread := range s.threadsFinish {
		if !thread {
			return
		}
	}
	s.nowSearchTrack = 0
	tracksChan := make(chan yandexmusic.SingleTrack)
	for i := 0; i < s.maxThreads; i++ {
		s.threadsFinish[i] = false
		go s.searchTracksInSpotifyChan(tracksChan, i)
	}
	for _, yt := range playlist.Tracks {
		if s.hasSpotifySearchResult(yt.ID) {
			continue
		}
		tracksChan <- yt
	}
	close(tracksChan)
}

// ServeHTTP передает net/http-запрос в роутер приложения.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// configureRouter регистрирует middleware и HTML-маршруты локального приложения.
func (s *server) configureRouter() {
	s.router.HandleFunc("GET /{$}", s.handleHome)
	s.router.HandleFunc("GET /login", s.handleLogin)
	s.router.HandleFunc("GET /logout", s.handleLogout)
	s.router.HandleFunc("GET "+callbackURI, s.handleCallbackSpotify)
	s.router.HandleFunc("GET /ya_music", s.handleYandexMusic)
	s.router.HandleFunc("GET /ya_music/create_playlist", s.handleCreatePlaylist)
	s.router.HandleFunc("GET /ya_music/add_to_liked", s.handleAddToLiked)
	s.router.HandleFunc("GET /ya_music/search", s.handleSearchOnSpotify)
	s.router.HandleFunc("GET /ya_music/playlists", s.handleSpotifyPlaylists)
	s.router.HandleFunc("GET /ya_music/playlists/{page}", s.handleSpotifyPlaylists)
	s.router.HandleFunc("GET /ya_music/liked_playlist", s.handleSpotifySaved)
	s.router.HandleFunc("GET /ya_music/playlist/{id}", s.handleSpotifyPlaylist)
	s.router.HandleFunc("GET /ya_music/playlist/{id}/{page}", s.handleSpotifyPlaylist)

	s.handler = s.setRequestID(s.logRequest(s.setContentTypeHTML(s.setAllowedOrigins(s.router))))
}

// setContentTypeHTML устанавливает HTML Content-Type для ответа и передает управление следующему обработчику.
//
// Parameters:
//   - next: следующий обработчик middleware chain.
//
// Returns:
//   - http.Handler: обработчик с HTML Content-Type.
func (s *server) setContentTypeHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

// setAllowedOrigins добавляет CORS-заголовки к ответу и продолжает обработку запроса.
//
// Parameters:
//   - next: следующий обработчик middleware chain.
//
// Returns:
//   - http.Handler: обработчик с CORS-заголовками.
func (s *server) setAllowedOrigins(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		next.ServeHTTP(w, r)
	})
}

// setRequestID создает request ID, записывает его в заголовок ответа и контекст запроса.
//
// Parameters:
//   - next: следующий обработчик middleware chain.
//
// Returns:
//   - http.Handler: обработчик с request ID в заголовке и контексте.
func (s *server) setRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// logRequest логирует начало и завершение HTTP-запроса.
//
// Parameters:
//   - next: следующий обработчик middleware chain.
//
// Returns:
//   - http.Handler: обработчик с логированием запроса и ответа.
func (s *server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusWriter := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		logger := s.logger.WithFields(logrus.Fields{
			"remote_addr": r.RemoteAddr,
			"request_id":  r.Context().Value(requestIDKey),
		})
		logger.Infof("started %s %s", r.Method, r.RequestURI)

		start := time.Now()
		next.ServeHTTP(statusWriter, r)
		statusCode := statusWriter.statusCode
		logger.Infof("completed with %s [%d] in %v", http.StatusText(statusCode), statusCode, time.Since(start))
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader сохраняет HTTP-статус для access log и передает его базовому ResponseWriter.
//
// Parameters:
//   - code: HTTP-статус ответа.
func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// handleCallbackSpotify обрабатывает OAuth callback от Spotify и сохраняет авторизованный клиент.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: callback-запрос от Spotify.
func (s *server) handleCallbackSpotify(w http.ResponseWriter, r *http.Request) {
	tok, err := s.auth.Token(r.Context(), s.state, r)
	if err != nil {
		s.error(w, http.StatusForbidden, err)
		return
	}
	if st := r.FormValue("state"); st != s.state {
		s.error(w, http.StatusNotFound, errors.New("invalid state"))
		return
	}

	client := spotify.New(s.auth.Client(r.Context(), tok), spotify.WithRetry(true))
	s.mClient.Lock()
	defer s.mClient.Unlock()
	s.spotifyClient = client
	if s.currentUser, err = s.spotifyClient.CurrentUser(r.Context()); err != nil {
		s.error(w, http.StatusUnprocessableEntity, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// getHeader собирает верхний HTML-блок с состоянием авторизации и навигацией.
//
// Returns:
//   - string: HTML-фрагмент для вставки на страницы приложения.
func (s *server) getHeader() string {
	inLink := "<a href=\"/login\">login</a>"
	outLink := "<a href=\"/logout\">logout</a>"
	playlistsLink := "<a href=\"/ya_music/playlists\">playlists</a>"
	savedPlaylistLink := "<a href=\"/ya_music/liked_playlist\">liked playlist</a>"
	page := ""
	if len(s.currentUser.ID) > 0 {
		page += fmt.Sprintf("User: %s<br/>ID: %s <br/>", s.currentUser.DisplayName, s.currentUser.ID)
		page += fmt.Sprintf("You can %s <br/>", outLink)
		page += fmt.Sprintf("You can watch spotify %s <br/>", playlistsLink)
		page += fmt.Sprintf("You can watch spotify %s", savedPlaylistLink)
	} else {
		page += fmt.Sprintf("You can %s", inLink)
	}
	return page
}

// handleHome отображает главную страницу локального приложения.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleHome(w http.ResponseWriter, _ *http.Request) {
	page := s.getHeader()
	page += "<br/>You can <a href=\"/ya_music\">import</a> playlist from Yandex.Music"
	s.respond(w, http.StatusOK, page)
}

// getYandexList собирает HTML-представление импортированного плейлиста и статуса поиска в Spotify.
//
// Returns:
//   - string: HTML-фрагмент со списком треков, счетчиками и доступными действиями.
//
// Cases:
//   - если плейлист еще не импортирован, возвращается пустая строка.
//   - если поиск завершен, добавляется ссылка на создание Spotify-плейлиста.
func (s *server) getYandexList() string {
	list := ""
	playlist, importing, importErr := s.yandexImportState()
	if importing {
		return "<p><strong>Importing Yandex playlist...</strong></p>"
	}
	if importErr != nil {
		return fmt.Sprintf("<p><strong>%v</strong></p>", importErr)
	}
	if playlist == nil {
		return list
	}
	list += fmt.Sprintf("<br><h3>Yandex playlist \"%s\" from %s with playlist_id %d</h3>",
		playlist.Title,
		playlist.Owner,
		playlist.PlaylistID,
	)
	tracksQuantity := s.quantityOfTracks()
	if tracksQuantity.yandex == 0 {
		return list
	}
	tracksList := "<ol>"
	for _, track := range playlist.Tracks {
		tracksList += "<li>"
		trackString := track.String()
		s.mMap.Lock()
		if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" {
			if spotifyTrack.ID == "nil" {
				trackString += " <b>Not found in Spotify</b>"
			} else {
				trackString += "Links: "
				trackString += fmt.Sprintf("<a target=\"blank\" href=\"%s\"><b>App</b></a> | ", spotifyTrack.URI)
				trackString += fmt.Sprintf(
					"<a target=\"blank\" href=\"%s\"><b>Web</b></a>",
					strings.Join(strings.Split(string(spotifyTrack.URI), ":")[1:], "/"),
				)
			}
		}
		s.mMap.Unlock()
		tracksList += trackString + "</li>"
	}
	tracksList += "</ol>"
	list += fmt.Sprintf("<h4>Tracks founded on yandex: %d</h4>", tracksQuantity.yandex)
	list += fmt.Sprintf("<h4>Tracks founded on spotify: %d</h4>", tracksQuantity.foundedSpotify)
	list += fmt.Sprintf("<h4>Tracks not found on spotify: %d</h4>", tracksQuantity.notFoundedSpotify)
	threadsFinished := 0
	for _, finish := range s.threadsFinish {
		if finish {
			threadsFinished++
		}
	}
	if threadsFinished == len(s.threadsFinish) {
		list += "<h5><a href=\"/ya_music/search\">Search on Spotify</a></h5>"
	}
	if tracksQuantity.foundedSpotify+tracksQuantity.notFoundedSpotify == tracksQuantity.yandex &&
		tracksQuantity.foundedSpotify > 0 {
		list += "<h5><a href=\"/ya_music/create_playlist\">Add playlist to Spotify</a></h5>"
		list += "<h5><a href=\"/ya_music/add_to_liked\">Add tracks to Spotify liked</a></h5>"
	}
	list += tracksList
	return list
}

// handleYandexMusic отображает форму импорта и загружает плейлист Яндекс Музыки по переданной ссылке.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleYandexMusic(w http.ResponseWriter, r *http.Request) {
	page := s.getHeader()
	importURL := r.URL.Query().Get("import_url")
	page += `
<form method="get">
<label>Playlist url: </label>
<input type="text" name="import_url"></input>
<button type="submit">Submit</button>
</form>
<p><a href="/">Home</a></p>
`
	if len(importURL) > 0 {
		var err error
		var playlist *yandexmusic.Playlist
		playlist, err = yandexmusic.NewPlaylistFromLink(importURL)
		if err != nil {
			page += fmt.Sprintf("<p><strong>%v</strong></p>", err)
		} else if playlist != nil {
			s.startYandexImport(playlist)
			http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
			return
		}
	}
	page += s.getYandexList()
	s.respond(w, http.StatusOK, page)
}

// handleCreatePlaylist создает Spotify-плейлист из найденных треков импортированного плейлиста.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	yandexPlaylist := s.yandexPlaylist()
	if yandexPlaylist == nil {
		http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
		return
	}
	page := "<form method=\"get\">"
	page += "<label>New playlist name: </label>"
	page += fmt.Sprintf("<input type=\"text\" name=\"playlist_name\" value=\"%s\" required>", yandexPlaylist.Title)
	page += "<label>New playlist description: </label>"
	page += fmt.Sprintf("<input type=\"text\" name=\"playlist_description\" value=\"%s\">", yandexPlaylist.Description)
	page += "<button type=\"submit\">Create</button>"
	page += "</form>"
	page += "<p><a href=\"/\">Home</a></p>"
	page += s.getYandexList()
	s.logger.Infoln(r.URL.Query())
	playlistName := r.URL.Query().Get("playlist_name")
	playlistDescription := r.URL.Query().Get("playlist_description")
	if len(playlistName) == 0 {
		s.respond(w, http.StatusOK, page)
		return
	}
	s.mClient.Lock()
	defer s.mClient.Unlock()
	if s.currentUser.ID == "" {
		s.logger.Infoln("not user id")
		page = "<p>Not user id!</p>" + page
		s.respond(w, http.StatusOK, page)
		return
	}
	playlist, err := s.spotifyClient.CreatePlaylist(
		r.Context(),
		playlistName,
		playlistDescription,
		false,
		false,
	)
	if err != nil {
		s.logger.Errorln(err)
		page = fmt.Sprintf("<p>%v</p>", err) + page
		s.respond(w, http.StatusOK, page)
		return
	}
	var trackURIs []spotify.URI
	for i, foundedTrack := range s.foundedTracks() {
		trackURIs = append(trackURIs, foundedTrack.URI)
		if (i+1)%100 == 0 {
			_, err = s.spotifyClient.AddItemsToPlaylist(r.Context(), playlist.ID, trackURIs...)
			if err != nil {
				s.logger.Errorln(err)
				page = fmt.Sprintf("<p>%v</p>", err) + page
				s.respond(w, http.StatusOK, page)
				return
			}
			trackURIs = []spotify.URI{}
		}
	}
	if len(trackURIs) > 0 {
		_, err = s.spotifyClient.AddItemsToPlaylist(r.Context(), playlist.ID, trackURIs...)
		if err != nil {
			s.logger.Errorln(err)
			page = fmt.Sprintf("<p>%v</p>", err) + page
			s.respond(w, http.StatusOK, page)
			return
		}
	}
	page = "<p>Success create playlist!</p>" + page
	s.respond(w, http.StatusOK, page)
}

// handleAddToLiked добавляет найденные треки Spotify в любимые треки пользователя.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleAddToLiked(w http.ResponseWriter, r *http.Request) {
	if s.yandexPlaylist() == nil {
		http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
		return
	}

	page := "<p><a href=\"/\">Home</a></p>"
	page += s.getYandexList()

	if s.currentUser.ID == "" {
		s.logger.Infoln("not user id")
		page = "<p>Not user id!</p>" + page
		s.respond(w, http.StatusOK, page)
		return
	}

	foundedTracks := s.foundedTracks()
	if len(foundedTracks) == 0 {
		page = "<p>No found tracks to add!</p>" + page
		s.respond(w, http.StatusOK, page)
		return
	}

	trackURIs := make([]spotify.URI, 0, len(foundedTracks))
	for _, foundedTrack := range foundedTracks {
		trackURIs = append(trackURIs, foundedTrack.URI)
	}

	client := s.spotifyClientSnapshot()
	for start := 0; start < len(trackURIs); start += spotifyLibraryBatchSize {
		end := min(start+spotifyLibraryBatchSize, len(trackURIs))
		if err := client.AddItemsToLibrary(r.Context(), trackURIs[start:end]...); err != nil {
			s.logger.Errorln(err)
			page = fmt.Sprintf("<p>%v</p>", err) + page
			s.respond(w, http.StatusOK, page)
			return
		}
	}

	page = "<p>Success add tracks to liked!</p>" + page
	s.respond(w, http.StatusOK, page)
}

// handleSearchOnSpotify запускает фоновый поиск импортированных треков в Spotify.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleSearchOnSpotify(w http.ResponseWriter, r *http.Request) {
	go s.searchTracksInSpotify()
	http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
}

const pageSize = 10

// handleSpotifyPlaylists отображает страницу со списком плейлистов авторизованного Spotify-пользователя.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request; path-параметр page задает страницу пагинации.
func (s *server) handleSpotifyPlaylists(w http.ResponseWriter, r *http.Request) {
	page := "<h1>Playlists</h1>"
	if s.currentUser == nil || s.currentUser.ID == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	pageNum := pageNumber(r.PathValue("page"))
	page += fmt.Sprintf("<h2>Page %d</h2>", pageNum)
	page += "<div>"
	playlists, err := s.spotifyClient.CurrentUsersPlaylists(
		r.Context(),
		spotify.Limit(pageSize),
		spotify.Offset(pageSize*(pageNum-1)),
	)
	if err != nil {
		page += err.Error()
		page += "</div>"
		s.respond(w, http.StatusInternalServerError, page)
		return
	}
	page += "<ul>"
	for _, pl := range playlists.Playlists {
		page += "<li>"
		page += fmt.Sprintf(`<a href="/ya_music/playlist/%s">%s</a>`, pl.ID, pl.Name)
		page += "</li>"
	}
	page += "</ul>"
	page += "</div>"
	page += "<div>"
	if pageNum > 1 {
		page += fmt.Sprintf(`<a href="/ya_music/playlists/%d">Prev</a>`, pageNum-1)
	}
	if pageSize*(pageNum-1)+len(playlists.Playlists) < int(playlists.Total) {
		page += fmt.Sprintf(`<a href="/ya_music/playlists/%d">Next</a>`, pageNum+1)
	}
	page += "</div>"

	s.respond(w, http.StatusOK, page)
}

// handleSpotifyPlaylist отображает треки выбранного Spotify-плейлиста.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request; path-параметры id и page задают плейлист и страницу.
func (s *server) handleSpotifyPlaylist(w http.ResponseWriter, r *http.Request) {
	page := "<h1>Playlist</h1>"
	if s.currentUser == nil || s.currentUser.ID == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	pageNum := pageNumber(r.PathValue("page"))
	idStr := r.PathValue("id")
	if idStr == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	page += fmt.Sprintf("<h2>ID %s</h2>", idStr)
	page += fmt.Sprintf("<h2>Page %d</h2>", pageNum)
	page += "<div>"
	playlist, err := s.spotifyClient.GetPlaylistTracks(
		r.Context(),
		spotify.ID(idStr),
		spotify.Limit(100),
		spotify.Offset(100*(pageNum-1)),
	)
	if err != nil {
		page += err.Error()
		page += "</div>"
		s.respond(w, http.StatusInternalServerError, page)
		return
	}
	page += "<ul>"
	for _, track := range playlist.Tracks {
		page += "<li>"
		artists := make([]string, len(track.Track.Artists))
		for i, art := range track.Track.Artists {
			artists[i] = art.Name
		}
		page += fmt.Sprintf("%s - %s", strings.Join(artists, ", "), track.Track.Name)
		page += "</li>"
	}
	page += "</ul>"
	page += "</div>"
	page += "<div>"
	if pageNum > 1 {
		page += fmt.Sprintf(`<a href="/ya_music/playlist/%s/%d">Prev</a>`, idStr, pageNum-1)
	}
	if 100*(pageNum-1)+len(playlist.Tracks) < int(playlist.Total) {
		page += fmt.Sprintf(`<a href="/ya_music/playlist/%s/%d">Next</a>`, idStr, pageNum+1)
	}
	page += "</div>"

	s.respond(w, http.StatusOK, page)
}

// handleSpotifySaved отображает все сохраненные треки авторизованного Spotify-пользователя.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleSpotifySaved(w http.ResponseWriter, r *http.Request) {
	page := "<h1>Liked Playlist</h1>"
	if s.currentUser == nil || s.currentUser.ID == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	pageNum := 1
	page += "<div>"
	page += "<ul>"
	for {
		playlist, err := s.spotifyClient.CurrentUsersTracks(
			r.Context(),
			spotify.Limit(pageSize),
			spotify.Offset(pageSize*(pageNum-1)),
		)
		if err != nil {
			break
		}
		for _, track := range playlist.Tracks {
			page += "<li>"
			artists := make([]string, len(track.Artists))
			for i, art := range track.Artists {
				artists[i] = art.Name
			}
			page += fmt.Sprintf("%s - %s", strings.Join(artists, ", "), track.Name)
			page += "</li>"
		}
		if pageSize*(pageNum-1)+len(playlist.Tracks) >= int(playlist.Total) {
			break
		}
		pageNum++
		time.Sleep(time.Millisecond * 100)
	}

	page += "</ul>"
	page += "</div>"

	s.respond(w, http.StatusOK, page)
}

// handleLogin перенаправляет пользователя на страницу Spotify OAuth.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	redirectURI := s.auth.AuthURL(s.state)
	http.Redirect(w, r, redirectURI, http.StatusTemporaryRedirect)
}

// handleLogout сбрасывает текущего Spotify-пользователя и клиент в состоянии сервера.
//
// Parameters:
//   - w: HTTP response writer.
//   - r: HTTP request.
func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.mClient.Lock()
	s.currentUser, s.spotifyClient = &spotify.PrivateUser{}, &spotify.Client{}
	s.mClient.Unlock()
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

// error отправляет JSON-ответ с текстом ошибки и заданным HTTP-статусом.
//
// Parameters:
//   - w: HTTP response writer.
//   - code: HTTP-статус ответа.
//   - err: ошибка для поля "error".
func (s *server) error(w http.ResponseWriter, code int, err error) {
	resp := map[string]string{"error": err.Error()}
	s.respondJSON(w, code, resp)
}

// respond отправляет строковый ответ с заданным HTTP-статусом.
//
// Parameters:
//   - w: HTTP response writer.
//   - code: HTTP-статус ответа.
//   - data: тело ответа.
func (s *server) respond(w http.ResponseWriter, code int, data string) {
	w.WriteHeader(code)
	_, _ = w.Write([]byte(data))
}

// respondJSON кодирует значение в JSON и отправляет его с заданным HTTP-статусом.
//
// Parameters:
//   - w: HTTP response writer.
//   - code: HTTP-статус ответа.
//   - data: значение для JSON-кодирования.
func (s *server) respondJSON(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error(err)
	}
}

// pageNumber преобразует path-параметр страницы в положительный номер страницы.
//
// Parameters:
//   - pageStr: значение path-параметра page.
//
// Returns:
//   - int: номер страницы; 1 для пустого, невалидного или нулевого значения.
func pageNumber(pageStr string) int {
	pageNum := 1
	if pageStr != "" {
		parsedPage, err := strconv.Atoi(pageStr)
		if parsedPage != 0 && err == nil {
			pageNum = parsedPage
		}
	}
	return pageNum
}
