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
	"strings"
	"sync"
	"time"
	"yandexToSpotify/yandex_music"
)

type yandexSpotifyTrackIdMap map[string]spotify.FullTrack

type server struct {
	router        *mux.Router
	logger        *logrus.Logger
	auth          *spotifyauth.Authenticator
	state         string
	spotifyClient *spotify.Client
	currentUser   *spotify.PrivateUser
	savedPlaylist *yandex_music.Playlist
	yandexSpotify yandexSpotifyTrackIdMap
	mMap          *sync.Mutex
	mCounter      [5]sync.Mutex
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
		mCounter:      [5]sync.Mutex{},
	}

	s.configureRouter()

	return s
}

func (s *server) tracksCount() chan *yandex_music.SingleTrack {
	ch := make(chan *yandex_music.SingleTrack)

	go func(ch chan *yandex_music.SingleTrack) {
		if s.savedPlaylist == nil {
			close(ch)
			return
		}
		for _, yt := range s.savedPlaylist.Tracks {
			ch <- &yt
			if s.savedPlaylist == nil {
				close(ch)
				return
			}
		}

		close(ch)
	}(ch)

	return ch
}

func (s *server) searchTrackInSpotify(yt *yandex_music.SingleTrack, ctx context.Context, thread int) {
	s.mCounter[thread].Lock()
	defer s.mCounter[thread].Unlock()
	if len(s.currentUser.ID) == 0 {
		return
	}
	var foundTrack spotify.FullTrack
	for _, artist := range yt.Artists {
		res, err := s.spotifyClient.Search(ctx, strings.Join([]string{yt.Title, artist.String()}, " "), spotify.SearchTypeTrack, spotify.Limit(10))
		if err != nil {
			continue
		}
		if tracks := res.Tracks.Tracks; len(tracks) > 0 {
			foundTrack = tracks[0]
			break
		}
	}
	foundTrack = spotify.FullTrack{SimpleTrack: spotify.SimpleTrack{ID: "nil"}}
	s.mMap.Lock()
	s.yandexSpotify[yt.ID] = foundTrack
	s.mMap.Unlock()
}

func (s *server) searchTracksInSpotify(ctx context.Context) {
	if len(s.currentUser.ID) == 0 {
		return
	}
	maxCount := len(s.mCounter)
	nowThread := 0
	for track := range s.tracksCount() {
		go s.searchTrackInSpotify(track, ctx, nowThread)
		nowThread += 1
		if nowThread >= maxCount {
			nowThread = 0
		}
	}
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
}

func (s *server) setContentTypeHTML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html")
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

		tok, err := s.auth.Token(r.Context(), s.state, r)
		if err != nil {
			s.error(w, r, http.StatusForbidden, err)
			return
		}
		//logrus.Infof("Token: %s", tok)
		if st := r.FormValue("state"); st != s.state {
			s.error(w, r, http.StatusNotFound, err)
			return
		}

		// use the token to get an authenticated client
		client := spotify.New(s.auth.Client(r.Context(), tok))
		s.spotifyClient = client
		if s.currentUser, err = s.spotifyClient.CurrentUser(r.Context()); err != nil {
			s.error(w, r, http.StatusUnprocessableEntity, err)
			return
		}
		//s.respond(w, r, http.StatusOK, "Login Completed!")
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

func (s *server) handleHome() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		//user, _ := s.spotifyClient.currentUser()
		inLink := "<a href=\"/login\">login</a>"
		outLink := "<a href=\"/logout\">logout</a>"
		page := ""
		if len(s.currentUser.ID) > 0 {
			page += fmt.Sprintf("User: %s<br/>ID: %s <br/>", s.currentUser.DisplayName, s.currentUser.ID)
		}
		page += fmt.Sprintf("You can %s or %s", inLink, outLink)
		page += "<br/>You can <a href=\"/ya_music\">import</a> playlist from Yandex.Music"
		s.respond(w, r, http.StatusOK, page)
	}
}

func (s *server) handleYandexMusic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		importUrl := r.URL.Query().Get("import_url")
		page := ""
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
		fmt.Println(s.savedPlaylist)
		if s.savedPlaylist != nil {
			page += fmt.Sprintf("<br><h3>Yandex playlist \"%s\" from %s with playlist_id %d</h3>",
				s.savedPlaylist.Title,
				s.savedPlaylist.Owner,
				s.savedPlaylist.PlaylistId,
			)
			tracksQuantity := len(s.savedPlaylist.Tracks)
			if tracksQuantity > 0 {
				page += fmt.Sprintf("<h4>Tracks founded: %d</h4>", tracksQuantity)
				page += fmt.Sprintf("<h5><a href=\"/ya_music/create_playlist\">Add playlist to Spotify</a></h5>")
				page += "<ol>"
				for _, track := range s.savedPlaylist.Tracks {
					page += "<li>"
					trackString := track.String()
					if spotifyTrack := s.yandexSpotify[track.ID]; spotifyTrack.ID != "" {
						if spotifyTrack.ID == "nil" {
							trackString += " <b>Not found in Spotify</b>"
						} else {
							trackString += fmt.Sprintf(" <a target=\"blank\" href=\"https://open.spotify.com/track/%s\"><b>Spotify link</b></a>", spotifyId)
						}
					} else {
					}
					page += trackString + "</li>"
				}
				page += "</ol>"
			}
		}
		s.respond(w, r, http.StatusOK, page)
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
		s.currentUser, s.spotifyClient = &spotify.PrivateUser{}, &spotify.Client{}
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
	_, _ = w.Write([]byte(string(EncodeWindows1251([]uint8(data)))))
}
