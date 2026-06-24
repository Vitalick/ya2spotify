package main

import (
	"flag"
	"fmt"
	"log"

	"yandexToSpotify/spotify_connect"
)

func main() {
	//playlistData := yandex_music.NewPlaylist(
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

	if err := spotify_connect.Start(); err != nil {
		log.Fatal(err)
	}
}
