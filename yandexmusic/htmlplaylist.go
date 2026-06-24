package yandexmusic

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/antchfx/htmlquery"
	"golang.org/x/net/html"
)

type stateUpdate struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func getScriptData(node *html.Node) ([]stateUpdate, error) {
	if node.Type != html.ElementNode {
		return nil, nil
	}
	if node.Data != "script" {
		return nil, nil
	}
	if node.FirstChild == nil {
		return nil, nil
	}
	if node.FirstChild != node.LastChild {
		return nil, nil
	}
	if node.FirstChild.Type != html.TextNode {
		return nil, nil
	}
	scriptData := node.FirstChild.Data
	if !strings.Contains(scriptData, "window.__STATE_PATCHES__") {
		return nil, nil
	}
	pattern, err := regexp.Compile(`(?s)\(window\.__STATE_PATCHES__\s*=\s*window\.__STATE_PATCHES__\s*\|\|\s*\[]\)\.push\((\[.*])\);`)
	if err != nil {
		return nil, err
	}
	result := pattern.FindAllSubmatch([]byte(scriptData), -1)
	if len(result) == 0 {
		return nil, nil
	}
	var resultArr []stateUpdate
	err = json.Unmarshal(result[0][1], &resultArr)
	if err != nil {
		return nil, err
	}
	return resultArr, nil
}

func getStatePatchScriptNodes(reader io.Reader) ([]stateUpdate, error) {
	res, err := htmlquery.Parse(reader)
	if err != nil {
		return nil, err
	}
	var arrays []stateUpdate
	var errList []error
	for _, script := range htmlquery.Find(res, "//script") {
		scriptData, err := getScriptData(script)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		if scriptData != nil {
			arrays = append(arrays, scriptData...)
		}
	}
	if len(errList) > 0 {
		return nil, errors.Join(errList...)
	}
	return arrays, nil
}

func getYandexState() {

}
