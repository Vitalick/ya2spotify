// Package main запускает локальное приложение ya2spotify.
package main

import (
	"flag"
	"fmt"
	"log"

	"ya2spotify/spotifyconnect"
)

// main запускает локальный веб-сервер для импорта плейлиста Яндекс Музыки в Spotify.
func main() {
	//playlistData := yandexmusic.NewPlaylist(
	//	"music-blog",
	//	2441,
	//)
	//if err := playlistData.GetTracks(); err != nil {
	//	log.Fatalln(err)
	//}
	//for i, track := range playlistData.Tracks {
	//	fmt.Println(i+1, track.String())
	//}
	fmt.Println("Started")
	flag.Parse()

	if err := spotifyconnect.Start(); err != nil {
		log.Fatal(err)
	}
}
