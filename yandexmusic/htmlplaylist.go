package yandexmusic

import (
	"fmt"
	"io"

	"github.com/antchfx/htmlquery"
)

func getStatePatchScriptNodes(reader io.Reader) error {
	res, err := htmlquery.Parse(reader)
	if err != nil {
		return err
	}
	fmt.Println(res)
	return nil
}
