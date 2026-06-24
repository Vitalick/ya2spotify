package yandexmusic

import (
	"fmt"
	"io"
	"strings"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
)

func getScriptData(node *html.Node) string {
	if node.Type != html.ElementNode {
		return ""
	}
	if node.Data != "script" {
		return ""
	}
	if node.FirstChild == nil {
		return ""
	}
	if node.FirstChild != node.LastChild {
		return ""
	}
	if node.FirstChild.Type != html.TextNode {
		return ""
	}
	scriptData := node.FirstChild.Data
	if !strings.Contains(scriptData, "window.__STATE_PATCHES__") {
		return ""
	}
	return scriptData
}

func getStatePatchScriptNodes(reader io.Reader) error {
	res, err := htmlquery.Parse(reader)
	if err != nil {
		return err
	}
	var scripts []string
	for _, script := range htmlquery.Find(res, "//script") {
		if scriptData := getScriptData(script); scriptData != "" {
			scripts = append(scripts, script.FirstChild.Data)
		}
	}
	fmt.Println(len(scripts))
	return nil
}
