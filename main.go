package main

import (
	"fmt"
	"log"
	"yandexToSpotify/yandex_music"
)

func main() {
	playlistData := yandex_music.NewPlaylist(
		"neumoin.vitaly",
		1018,
	)
	if err := playlistData.GetTracks(); err != nil {
		log.Fatalln(err)
	}
	for i, track := range playlistData.Tracks {
		fmt.Println(i+1, track.String())
	}
}
