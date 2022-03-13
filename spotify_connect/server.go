package spotify_connect

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	routing "github.com/qiangxue/fasthttp-routing"
	"github.com/sirupsen/logrus"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttpadaptor"
	"github.com/vitalick/go-d2editor"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"yandexToSpotify/yandex_music"
)

type yandexSpotifyTrackIdMap map[string]spotify.FullTrack

type server struct {
	router         *routing.Router
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

func newServer(auth *spotifyauth.Authenticator, state string) *server {
	s := &server{
		router:        routing.New(),
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

func (s *server) ServeHTTP(ctx *fasthttp.RequestCtx) {
	s.router.HandleRequest(ctx)
}

func (s *server) configureRouter() {
	s.router.Use(s.setRequestID)
	s.router.Use(s.logRequest)
	s.router.Use(s.setContentTypeHTML)
	s.router.Use(s.setAllowedOrigins)
	s.router.Get("/", s.handleHome)
	s.router.Get("/login", s.handleLogin)
	s.router.Get("/logout", s.handleLogout)
	s.router.Get(callbackUri, s.handleCallbackSpotify)
	yaMusic := s.router.Group("/ya_music")
	//yaMusic.Use(s.authenticateUser)
	yaMusic.Get("", s.handleYandexMusic)
	yaMusic.Get("/create_playlist", s.handleCreatePlaylist)
	yaMusic.Get("/search", s.handleSearchOnSpotify)
	yaMusic.Get("/playlists", s.handleSpotifyPlaylists)
	yaMusic.Get("/playlists/<page>", s.handleSpotifyPlaylists)
	yaMusic.Get("/liked_playlist", s.handleSpotifySaved)
	yaMusic.Get("/playlist/<id>", s.handleSpotifyPlaylist)
	yaMusic.Get("/playlist/<id>/<page>", s.handleSpotifyPlaylist)
	yaMusic.Post("/testd2s", s.setContentTypeJSON, s.handleTestD2s)
}

func (s *server) setContentTypeHTML(ctx *routing.Context) error {
	ctx.Response.Header.Add("Content-Type", "text/html; charset=utf-8")
	err := ctx.Next()
	if err != nil {
		return err
	}
	return nil
}

func (s *server) setContentTypeJSON(ctx *routing.Context) error {
	ctx.Response.Header.Set("Content-Type", "application/json; charset=utf-8")
	err := ctx.Next()
	if err != nil {
		return err
	}
	return nil
}

func (s *server) setAllowedOrigins(ctx *routing.Context) error {
	ctx.Response.Header.Set("Access-Control-Allow-Credentials", "true")
	ctx.Response.Header.Set("Access-Control-Allow-Origin", "*")
	err := ctx.Next()
	if err != nil {
		return err
	}
	return nil
}

func (s *server) setRequestID(ctx *routing.Context) error {
	id := uuid.New().String()
	ctx.Response.Header.Set("X-Request-ID", id)
	ctx.Set("requestId", id)
	err := ctx.Next()
	if err != nil {
		return err
	}
	return nil
}

func (s *server) logRequest(ctx *routing.Context) error {
	logger := s.logger.WithFields(logrus.Fields{
		"remote_addr": ctx.RemoteAddr(),
		"request_id":  ctx.Get("requestId"),
	})
	logger.Infof("started %s %s", string(ctx.Method()), string(ctx.RequestURI()))

	start := time.Now()

	ctx.SetStatusCode(fasthttp.StatusOK)

	err := ctx.Next()
	if err != nil {
		return err
	}
	statusCode := ctx.Response.StatusCode()
	logger.Infof("complited with %s [%d] in %v", http.StatusText(statusCode), statusCode, time.Now().Sub(start))
	return nil
}

func (s *server) handleCallbackSpotify(ctx *routing.Context) error {
	var httpRequest http.Request
	err := fasthttpadaptor.ConvertRequest(ctx.RequestCtx, &httpRequest, false)
	if err != nil {
		s.error(ctx, http.StatusForbidden, err)
		return nil
	}
	tok, err := s.auth.Token(ctx, s.state, &httpRequest)
	if err != nil {
		s.error(ctx, http.StatusForbidden, err)
		return nil
	}
	if st := string(ctx.FormValue("state")); st != s.state {
		s.error(ctx, http.StatusNotFound, err)
		return nil
	}

	// use the token to get an authenticated client
	client := spotify.New(s.auth.Client(ctx, tok))
	s.mClient.Lock()
	s.spotifyClient = client
	if s.currentUser, err = s.spotifyClient.CurrentUser(ctx); err != nil {
		s.error(ctx, http.StatusUnprocessableEntity, err)
		return nil
	}
	s.mClient.Unlock()
	//s.respond(w, r, http.StatusOK, "Login Completed!")
	ctx.Redirect("/", http.StatusTemporaryRedirect)
	return nil
}

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

func (s *server) handleHome(ctx *routing.Context) error {
	page := s.getHeader()
	page += "<br/>You can <a href=\"/ya_music\">import</a> playlist from Yandex.Music"
	s.respond(ctx, http.StatusOK, page)
	return nil
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

func (s *server) handleYandexMusic(ctx *routing.Context) error {
	page := s.getHeader()
	importUrl := string(ctx.QueryArgs().Peek("import_url"))
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
			ctx.Redirect("/ya_music", http.StatusTemporaryRedirect)
			return nil
		}
	}
	page += s.getYandexList()
	s.respond(ctx, http.StatusOK, page)
	return nil
}

func (s *server) handleCreatePlaylist(ctx *routing.Context) error {
	if s.savedPlaylist == nil {
		ctx.Redirect("/ya_music", http.StatusTemporaryRedirect)
		return nil
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
	s.logger.Infoln(ctx.QueryArgs())
	playlistName := string(ctx.QueryArgs().Peek("playlist_name"))
	playlistDescription := string(ctx.QueryArgs().Peek("playlist_description"))
	if len(playlistName) == 0 {
		s.respond(ctx, http.StatusOK, page)
		return nil
	}
	s.mClient.Lock()
	defer s.mClient.Unlock()
	if s.currentUser.ID == "" {
		s.logger.Infoln("not user id")
		page = "<p>Not user id!</p>" + page
		s.respond(ctx, http.StatusOK, page)
		return nil
	}
	playlist, err := s.spotifyClient.CreatePlaylistForUser(context.Background(), s.currentUser.ID, playlistName, playlistDescription, false, false)
	if err != nil {
		s.logger.Errorln(err)
		page = fmt.Sprintf("<p>%v</p>", err) + page
		s.respond(ctx, http.StatusOK, page)
		return nil
	}
	var trackIds []spotify.ID
	for i, foundedTrack := range s.foundedTracks() {
		trackIds = append(trackIds, foundedTrack.ID)
		if (i+1)%100 == 0 {
			_, err = s.spotifyClient.AddTracksToPlaylist(context.Background(), playlist.ID, trackIds...)
			if err != nil {
				s.logger.Errorln(err)
				page = fmt.Sprintf("<p>%v</p>", err) + page
				s.respond(ctx, http.StatusOK, page)
				return nil
			}
			trackIds = []spotify.ID{}
		}
	}
	if len(trackIds) > 0 {
		_, err = s.spotifyClient.AddTracksToPlaylist(context.Background(), playlist.ID, trackIds...)
		if err != nil {
			s.logger.Errorln(err)
			page = fmt.Sprintf("<p>%v</p>", err) + page
			s.respond(ctx, http.StatusOK, page)
			return nil
		}
	}
	page = "<p>Success create playlist!</p>" + page
	s.respond(ctx, http.StatusOK, page)
	return nil
}

func (s *server) handleSearchOnSpotify(ctx *routing.Context) error {
	go s.searchTracksInSpotify()
	ctx.Redirect("/ya_music", http.StatusTemporaryRedirect)
	return nil
}

const pageSize = 10

func (s *server) handleSpotifyPlaylists(ctx *routing.Context) error {
	page := "<h1>Playlists</h1>"
	var err error = nil
	if s.currentUser == nil || s.currentUser.ID == "" {
		ctx.Redirect("/", http.StatusTemporaryRedirect)
		return nil
	}
	pageNum := 1
	pageStr := ctx.Param("page")
	if pageStr != "" {
		pageNum, err = strconv.Atoi(pageStr)
		if pageNum == 0 || err != nil {
			pageNum = 1
		}
	}
	page += fmt.Sprintf("<h2>Page %d</h2>", pageNum)
	page += "<div>"
	playlists, err := s.spotifyClient.CurrentUsersPlaylists(context.Background(), spotify.Limit(pageSize), spotify.Offset(pageSize*(pageNum-1)))
	if err != nil {
		page += err.Error()
		page += "</div>"
		s.respond(ctx, http.StatusInternalServerError, page)
		return nil
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
	if pageSize*(pageNum-1)+len(playlists.Playlists) < playlists.Total {
		page += fmt.Sprintf(`<a href="/ya_music/playlists/%d">Next</a>`, pageNum+1)
	}
	page += "</div>"

	s.respond(ctx, http.StatusOK, page)
	return nil
}

func (s *server) handleSpotifyPlaylist(ctx *routing.Context) error {
	page := "<h1>Playlist</h1>"
	var err error = nil
	if s.currentUser == nil || s.currentUser.ID == "" {
		ctx.Redirect("/", http.StatusTemporaryRedirect)
		return nil
	}
	pageNum := 1
	pageStr := ctx.Param("page")
	if pageStr != "" {
		pageNum, err = strconv.Atoi(pageStr)
		if pageNum == 0 || err != nil {
			pageNum = 1
		}
	}
	idStr := ctx.Param("id")
	if idStr == "" {
		ctx.Redirect("/", http.StatusTemporaryRedirect)
		return nil
	}
	page += fmt.Sprintf("<h2>ID %s</h2>", idStr)
	page += fmt.Sprintf("<h2>Page %d</h2>", pageNum)
	page += "<div>"
	playlist, err := s.spotifyClient.GetPlaylistTracks(context.Background(), spotify.ID(idStr), spotify.Limit(100), spotify.Offset(100*(pageNum-1)))
	if err != nil {
		page += err.Error()
		page += "</div>"
		s.respond(ctx, http.StatusInternalServerError, page)
		return nil
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
	if 100*(pageNum-1)+len(playlist.Tracks) < playlist.Total {
		page += fmt.Sprintf(`<a href="/ya_music/playlist/%s/%d">Next</a>`, idStr, pageNum+1)
	}
	page += "</div>"

	s.respond(ctx, http.StatusOK, page)
	return nil
}

func (s *server) handleSpotifySaved(ctx *routing.Context) error {
	page := "<h1>Liked Playlist</h1>"
	if s.currentUser == nil || s.currentUser.ID == "" {
		ctx.Redirect("/", http.StatusTemporaryRedirect)
		return nil
	}
	pageNum := 1
	page += "<div>"
	page += "<ul>"
	for {
		playlist, err := s.spotifyClient.CurrentUsersTracks(context.Background(), spotify.Limit(pageSize), spotify.Offset(pageSize*(pageNum-1)))
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
		if pageSize*(pageNum-1)+len(playlist.Tracks) >= playlist.Total {
			break
		}
		pageNum += 1
		time.Sleep(time.Millisecond * 100)
	}

	page += "</ul>"
	page += "</div>"

	s.respond(ctx, http.StatusOK, page)
	return nil
}

func (s *server) handleTestD2s(ctx *routing.Context) error {
	file, err := ctx.FormFile("d2s")
	if err != nil {
		s.error(ctx, fasthttp.StatusUnprocessableEntity, err)
		return nil
	}
	f, err := file.Open()
	if err != nil {
		s.error(ctx, fasthttp.StatusUnprocessableEntity, err)
		return nil
	}
	defer f.Close()
	c, err := d2editor.NewCharacter(f)
	if err != nil {
		s.error(ctx, fasthttp.StatusUnprocessableEntity, err)
		return nil
	}
	s.respondJSON(ctx, fasthttp.StatusOK, c)
	return nil
}

func (s *server) handleLogin(ctx *routing.Context) error {
	redirectUri := s.auth.AuthURL(s.state)
	ctx.Redirect(redirectUri, http.StatusTemporaryRedirect)
	return nil
}

func (s *server) handleLogout(ctx *routing.Context) error {
	s.mClient.Lock()
	s.currentUser, s.spotifyClient = &spotify.PrivateUser{}, &spotify.Client{}
	s.mClient.Unlock()
	ctx.Redirect("/", http.StatusTemporaryRedirect)
	return nil
}

func (s *server) error(ctx *routing.Context, code int, err error) {
	resp := map[string]string{"error": err.Error()}
	marshal, _ := json.Marshal(resp)
	s.respond(ctx, code, string(marshal))
}

func (s *server) respond(ctx *routing.Context, code int, data string) {
	ctx.SetStatusCode(code)
	_, _ = ctx.WriteString(data)
}

func (s *server) respondJSON(ctx *routing.Context, code int, data interface{}) {
	ctx.SetStatusCode(code)
	enc := json.NewEncoder(ctx)
	enc.Encode(data)
	//_, _ = ctx.WriteString(b)
}
