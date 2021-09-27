package spotify_connect

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
	"yandexToSpotify/yandex_music"
)

type yandexSpotifyTrackIdMap map[string]spotify.FullTrack

type server struct {
	router         *mux.Router
	logger         *logrus.Logger
	auth           *spotifyauth.Authenticator
	state          string
	spotifyClient  *spotify.Client
	currentUser    *spotify.PrivateUser
	savedPlaylist  *yandex_music.Playlist
	yandexSpotify  yandexSpotifyTrackIdMap
	mMap           *sync.Mutex
	mCounter       *sync.Mutex
	mClient        *sync.Mutex
	maxThreads     int
	threadsFinish  []bool
	nowSearchTrack int
}

const (
	ctxKeyRequestId ctxKey = iota
)

type ctxKey int8

func newServer(auth *spotifyauth.Authenticator, state string) *server {
	s := &server{
		router:        mux.NewRouter(),
		logger:        logrus.New(),
		auth:          auth,
		state:         state,
		spotifyClient: &spotify.Client{},
		currentUser:   &spotify.PrivateUser{},
		savedPlaylist: nil,
		yandexSpotify: make(yandexSpotifyTrackIdMap),
		mMap:          &sync.Mutex{},
		mCounter:      &sync.Mutex{},
		mClient:       &sync.Mutex{},
		maxThreads:    runtime.NumCPU(),
	}
	for i := 0; i < s.maxThreads; i++ {
		s.threadsFinish = append(
			s.threadsFinish, true)
	}
	s.configureRouter()

	return s
}

type tracksQuantity struct {
	yandex, foundedSpotify, notFoundedSpotify int
}

func (s *server) quantityOfTracks() *tracksQuantity {
	tracksQuantityYandex := len(s.savedPlaylist.Tracks)
	tracksFoundedSpotify := 0
	tracksNotFoundedSpotify := 0
	for _, track := range s.savedPlaylist.Tracks {
		s.mMap.Lock()
		if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" {
			if spotifyTrack.ID == "nil" {
				tracksNotFoundedSpotify += 1
			} else {
				tracksFoundedSpotify += 1
			}
		}
		s.mMap.Unlock()
	}
	return &tracksQuantity{tracksQuantityYandex, tracksFoundedSpotify, tracksNotFoundedSpotify}
}

func (s *server) foundedTracks() []spotify.FullTrack {
	var foundedTracks []spotify.FullTrack
	for _, track := range s.savedPlaylist.Tracks {
		s.mMap.Lock()
		if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" && spotifyTrack.ID != "nil" {
			foundedTracks = append(foundedTracks, spotifyTrack)
		}
		s.mMap.Unlock()
	}
	return foundedTracks
}

func (s *server) getTrack() *yandex_music.SingleTrack {
	s.mCounter.Lock()
	defer s.mCounter.Unlock()
	if s.nowSearchTrack >= len(s.savedPlaylist.Tracks) {
		return nil
	}
	s.nowSearchTrack += 1
	return &s.savedPlaylist.Tracks[s.nowSearchTrack-1]
}

func (s *server) searchTrackInSpotify(yt *yandex_music.SingleTrack) {
	ctx := context.Background()
	//s.logger.Infof("SEARCH %s\n", yt.String())
	if len(s.currentUser.ID) == 0 {
		return
	}
	var foundTrack spotify.FullTrack
	for _, artist := range yt.Artists {
		searchString := strings.Join([]string{yt.Title, artist.String()}, " ")
		s.mClient.Lock()
		res, err := s.spotifyClient.Search(ctx, searchString, spotify.SearchTypeTrack, spotify.Limit(10))
		s.mClient.Unlock()
		if err != nil {
			s.logger.Error(err)
			continue
		}
		//s.logger.Infoln(res)
		if tracks := res.Tracks.Tracks; len(tracks) > 0 {
			foundTrack = tracks[0]
			//s.logger.Infof("FOUND %s [%s]\n", yt.String(), searchString)
			break
		}
	}
	if len(foundTrack.ID) == 0 {
		//s.logger.Infof("NOT FOUND %s\n", yt.String())
		foundTrack = spotify.FullTrack{SimpleTrack: spotify.SimpleTrack{ID: "nil"}}
	}
	s.mMap.Lock()
	s.yandexSpotify[yt.ID] = foundTrack
	s.mMap.Unlock()
	//time.Sleep(time.Second * 1)
}

func (s *server) searchTracksInSpotifyChan(tracksChan chan yandex_music.SingleTrack, thread int) {
	for yt := range tracksChan {
		if yt.ID == "" {
			break
		}
		s.searchTrackInSpotify(&yt)
	}
	s.threadsFinish[thread] = true
}

func (s *server) searchTracksInSpotify() {
	if len(s.currentUser.ID) == 0 {
		return
	}
	for _, thread := range s.threadsFinish {
		if !thread {
			return
		}
	}
	s.nowSearchTrack = 0
	tracksChan := make(chan yandex_music.SingleTrack)
	for i := 0; i < s.maxThreads; i++ {
		s.threadsFinish[i] = false
		go s.searchTracksInSpotifyChan(tracksChan, i)
	}
	for _, yt := range s.savedPlaylist.Tracks {
		tracksChan <- yt
	}
	close(tracksChan)

}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *server) configureRouter() {
	s.router.Use(s.setRequestID)
	s.router.Use(s.logRequest)
	s.router.Use(s.setContentTypeHTML)
	s.router.Use(handlers.CORS(handlers.AllowedOrigins([]string{"*"})))
	s.router.HandleFunc("/", s.handleHome()).Methods(http.MethodGet)
	s.router.HandleFunc("/login", s.handleLogin()).Methods(http.MethodGet)
	s.router.HandleFunc("/logout", s.handleLogout()).Methods(http.MethodGet)
	s.router.HandleFunc(callbackUri, s.handleCallbackSpotify()).Methods(http.MethodGet)
	private := s.router.PathPrefix("/ya_music").Subrouter()
	//private.Use(s.authenticateUser)
	private.HandleFunc("", s.handleYandexMusic()).Methods(http.MethodGet)
	private.HandleFunc("/create_playlist", s.handleCreatePlaylist()).Methods(http.MethodGet)
	private.HandleFunc("/search", s.handleSearchOnSpotify()).Methods(http.MethodGet)
}

func (s *server) setContentTypeHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func (s *server) setRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.New().String()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestId, id)))
	})
}

func (s *server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := s.logger.WithFields(logrus.Fields{
			"remote_addr": r.RemoteAddr,
			"request_id":  r.Context().Value(ctxKeyRequestId),
		})
		logger.Infof("started %s %s", r.Method, r.RequestURI)

		start := time.Now()

		rw := &responseWriter{w, http.StatusOK}

		next.ServeHTTP(rw, r)

		logger.Infof("complited with %s [%d] in %v", http.StatusText(rw.code), rw.code, time.Now().Sub(start))
	})
}

func (s *server) handleCallbackSpotify() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tok, err := s.auth.Token(r.Context(), s.state, r)
		if err != nil {
			s.error(w, r, http.StatusForbidden, err)
			return
		}
		if st := r.FormValue("state"); st != s.state {
			s.error(w, r, http.StatusNotFound, err)
			return
		}

		// use the token to get an authenticated client
		client := spotify.New(s.auth.Client(ctx, tok))
		s.mClient.Lock()
		s.spotifyClient = client
		if s.currentUser, err = s.spotifyClient.CurrentUser(ctx); err != nil {
			s.error(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		s.mClient.Unlock()
		//s.respond(w, r, http.StatusOK, "Login Completed!")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

func (s *server) getHeader() string {
	inLink := "<a href=\"/login\">login</a>"
	outLink := "<a href=\"/logout\">logout</a>"
	page := ""
	if len(s.currentUser.ID) > 0 {
		page += fmt.Sprintf("User: %s<br/>ID: %s <br/>", s.currentUser.DisplayName, s.currentUser.ID)
		//s.mClient.Lock()
		//currentPlay, err := s.spotifyClient.PlayerCurrentlyPlaying(ctx)
		//s.mClient.Unlock()
		//if err != nil {
		//	page += "Error in retrieving now playing!</br>"
		//	s.logger.Error(err)
		//} else {
		//	if currentPlay.Playing {
		//		var artists []string
		//		for _, artist := range currentPlay.Item.Artists {
		//			artists = append(artists, fmt.Sprintf("<a target=\"_blank_\" href=\"%s\">%s</a>", artist.URI, artist.Name))
		//		}
		//		trackName := fmt.Sprintf("<a target=\"_blank_\" href=\"%s\">%s</a>", currentPlay.Item.URI, currentPlay.Item.Name)
		//		page += fmt.Sprintf("Now playing: %s - %s<br/>", strings.Join(artists, ", "), trackName)
		//	} else {
		//		page += "Now playing nothing!</br>"
		//	}
		//
		//}
		page += fmt.Sprintf("You can %s", outLink)
	} else {
		page += fmt.Sprintf("You can %s", inLink)
	}
	return page
}

func (s *server) handleHome() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := s.getHeader()
		page += "<br/>You can <a href=\"/ya_music\">import</a> playlist from Yandex.Music"
		s.respond(w, r, http.StatusOK, page)
	}
}

func (s *server) getYandexList() string {
	list := ""
	if s.savedPlaylist == nil {
		return list
	}
	list += fmt.Sprintf("<br><h3>Yandex playlist \"%s\" from %s with playlist_id %d</h3>",
		s.savedPlaylist.Title,
		s.savedPlaylist.Owner,
		s.savedPlaylist.PlaylistId,
	)
	tracksQuantity := s.quantityOfTracks()
	if tracksQuantity.yandex == 0 {
		return list
	}
	tracksList := "<ol>"
	for _, track := range s.savedPlaylist.Tracks {
		tracksList += "<li>"
		trackString := track.String()
		s.mMap.Lock()
		if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" {
			if spotifyTrack.ID == "nil" {
				trackString += " <b>Not found in Spotify</b>"
			} else {
				trackString += fmt.Sprintf(" <a target=\"blank\" href=\"%s\"><b>Spotify link</b></a>", spotifyTrack.URI)
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
			threadsFinished += 1
		}
	}
	if threadsFinished == len(s.threadsFinish) {
		list += fmt.Sprintf("<h5><a href=\"/ya_music/search\">Search on Spotify</a></h5>")
	}
	if tracksQuantity.foundedSpotify+tracksQuantity.notFoundedSpotify == tracksQuantity.yandex && tracksQuantity.foundedSpotify > 0 {
		list += fmt.Sprintf("<h5><a href=\"/ya_music/create_playlist\">Add playlist to Spotify</a></h5>")
	}
	list += tracksList
	return list
}

func (s *server) handleYandexMusic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := s.getHeader()
		importUrl := r.URL.Query().Get("import_url")
		page += `
<form method="get">
<label>Playlist url: </label>
<input type="text" name="import_url"></input>
<button type="submit">Submit</button>
</form>
<p><a href="/">Home</a></p>
`
		if len(importUrl) > 0 {
			var err error
			var playlist *yandex_music.Playlist
			if playlist, err = yandex_music.NewPlaylistFromLink(importUrl); err == nil && playlist != nil {
				err = playlist.GetTracks()
			}
			if err != nil {
				page += fmt.Sprintf("<p><strong>%v</strong></p>", err)
			}
			if err == nil {
				if playlist != nil {
					s.savedPlaylist = playlist
				}
				http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
				return
			}
		}
		page += s.getYandexList()
		s.respond(w, r, http.StatusOK, page)
	}
}

func (s *server) handleCreatePlaylist() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.savedPlaylist == nil {
			http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
			return
		}
		page := "<form method=\"get\">"
		page += "<label>New playlist name: </label>"
		page += fmt.Sprintf("<input type=\"text\" name=\"playlist_name\" value=\"%s\" required>",
			s.savedPlaylist.Title,
		)
		page += "<label>New playlist description: </label>"
		page += fmt.Sprintf("<input type=\"text\" name=\"playlist_description\" value=\"%s\">",
			s.savedPlaylist.Description,
		)
		page += "<button type=\"submit\">Create</button>"
		page += "</form>"
		page += "<p><a href=\"/\">Home</a></p>"
		page += s.getYandexList()
		s.logger.Infoln(r.URL.Query())
		playlistName := r.URL.Query().Get("playlist_name")
		playlistDescription := r.URL.Query().Get("playlist_description")
		if len(playlistName) == 0 {
			s.respond(w, r, http.StatusOK, page)
			return
		}
		s.mClient.Lock()
		defer s.mClient.Unlock()
		if s.currentUser.ID == "" {
			s.logger.Infoln("not user id")
			page = "<p>Not user id!</p>" + page
			s.respond(w, r, http.StatusOK, page)
			return
		}
		playlist, err := s.spotifyClient.CreatePlaylistForUser(context.Background(), s.currentUser.ID, playlistName, playlistDescription, false, false)
		if err != nil {
			s.logger.Errorln(err)
			page = fmt.Sprintf("<p>%v</p>", err) + page
			s.respond(w, r, http.StatusOK, page)
			return
		}
		var trackIds []spotify.ID
		for _, foundedTrack := range s.foundedTracks() {
			trackIds = append(trackIds, foundedTrack.ID)
		}
		_, err = s.spotifyClient.AddTracksToPlaylist(context.Background(), playlist.ID, trackIds...)
		if err != nil {
			s.logger.Errorln(err)
			page = fmt.Sprintf("<p>%v</p>", err) + page
			s.respond(w, r, http.StatusOK, page)
			return
		}
		page = "<p>Success create playlist!</p>" + page
		s.respond(w, r, http.StatusOK, page)
	}
}

func (s *server) handleSearchOnSpotify() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go s.searchTracksInSpotify()
		http.Redirect(w, r, "/ya_music", http.StatusTemporaryRedirect)
	}
}

func (s *server) handleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		redirectUri := s.auth.AuthURL(s.state)
		http.Redirect(w, r, redirectUri, http.StatusTemporaryRedirect)
	}
}

func (s *server) handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mClient.Lock()
		s.currentUser, s.spotifyClient = &spotify.PrivateUser{}, &spotify.Client{}
		s.mClient.Unlock()
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

func (s *server) error(w http.ResponseWriter, r *http.Request, code int, err error) {
	resp := map[string]string{"error": err.Error()}
	marshal, _ := json.Marshal(resp)
	s.respond(w, r, code, string(marshal))
}

func (s *server) respond(w http.ResponseWriter, _ *http.Request, code int, data string) {
	w.WriteHeader(code)
	_, _ = w.Write([]byte(data))
}
