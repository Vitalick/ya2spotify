package yandexmusic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
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
	pattern, err := regexp.Compile(
		`(?s)\(window\.__STATE_PATCHES__\s*=\s*window\.__STATE_PATCHES__\s*\|\|\s*\[]\)\.push\((\[.*])\);`,
	)
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

func getStateUpdateListFromFile(path string) ([]stateUpdate, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return getStateUpdateList(file)
}

func getStateUpdateListFromURL(url string) ([]stateUpdate, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return getStateUpdateList(resp.Body)
}

func getStateUpdateList(reader io.Reader) ([]stateUpdate, error) {
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

func getYandexState(arr []stateUpdate) (map[string]any, error) {
	var state any = make(map[string]any)
	for _, update := range arr {
		switch update.Op {
		case "replace", "add", "remove":
			path, err := parseStatePath(update.Path)
			if err != nil {
				return nil, err
			}
			state, err = applyStateUpdate(state, path, update.Value, update.Op)
			if err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown operation %s", update.Op)
		}
	}

	result, ok := state.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected root state type %T", state)
	}
	return result, nil
}

func parseStatePath(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("invalid state path %q", path)
	}

	parts := strings.Split(path[1:], "/")
	for i, part := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func applyStateUpdate(current any, path []string, value any, op string) (any, error) {
	if len(path) == 0 {
		return cloneStateValue(value), nil
	}

	key := path[0]
	if index, ok, err := parseStateArrayIndex(key); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return applyStateArrayUpdate(current, index, path, value, op)
	}

	obj, ok := current.(map[string]any)
	if !ok {
		if current != nil {
			return nil, fmt.Errorf("path /%s expects object, got %T", strings.Join(path, "/"), current)
		}
		obj = make(map[string]any)
	}

	if len(path) == 1 {
		if op == "remove" {
			if _, ok := obj[key]; !ok {
				return nil, fmt.Errorf("remove path /%s does not exist", strings.Join(path, "/"))
			}
			delete(obj, key)
			return obj, nil
		}
		obj[key] = cloneStateValue(value)
		return obj, nil
	}

	next, ok := obj[key]
	if (!ok || next == nil) && op == "remove" {
		return nil, fmt.Errorf("remove path /%s does not exist", strings.Join(path, "/"))
	}
	if !ok || next == nil {
		next = newStateContainer(path[1])
	}

	updated, err := applyStateUpdate(next, path[1:], value, op)
	if err != nil {
		return nil, err
	}
	obj[key] = updated
	return obj, nil
}

func applyStateArrayUpdate(current any, index int, path []string, value any, op string) ([]any, error) {
	arr, ok := current.([]any)
	if !ok {
		if current != nil {
			return nil, fmt.Errorf("path /%s expects array, got %T", strings.Join(path, "/"), current)
		}
		arr = make([]any, 0, index+1)
	}

	if index == -1 {
		if op != "add" || len(path) != 1 {
			return nil, fmt.Errorf("array append segment is only supported for add operations")
		}
		index = len(arr)
	}

	if len(path) == 1 {
		switch op {
		case "add":
			if index > len(arr) {
				return nil, fmt.Errorf("add index %d is out of bounds for path /%s", index, strings.Join(path, "/"))
			}
			arr = append(arr, nil)
			copy(arr[index+1:], arr[index:])
			arr[index] = cloneStateValue(value)
		case "replace":
			if index > len(arr) {
				return nil, fmt.Errorf("replace index %d is out of bounds for path /%s", index, strings.Join(path, "/"))
			}
			if index == len(arr) {
				arr = append(arr, cloneStateValue(value))
			} else {
				arr[index] = cloneStateValue(value)
			}
		case "remove":
			if index >= len(arr) {
				return nil, fmt.Errorf("remove index %d is out of bounds for path /%s", index, strings.Join(path, "/"))
			}
			arr = append(arr[:index], arr[index+1:]...)
		}
		return arr, nil
	}

	if index >= len(arr) && op == "remove" {
		return nil, fmt.Errorf("remove path /%s does not exist", strings.Join(path, "/"))
	}
	for len(arr) <= index {
		arr = append(arr, nil)
	}
	if arr[index] == nil {
		if op == "remove" {
			return nil, fmt.Errorf("remove path /%s does not exist", strings.Join(path, "/"))
		}
		arr[index] = newStateContainer(path[1])
	}

	updated, err := applyStateUpdate(arr[index], path[1:], value, op)
	if err != nil {
		return nil, err
	}
	arr[index] = updated
	return arr, nil
}

func parseStateArrayIndex(key string) (int, bool, error) {
	if key == "-" {
		return -1, true, nil
	}
	if key == "" {
		return 0, false, nil
	}
	index, err := strconv.Atoi(key)
	if err != nil {
		return 0, false, nil
	}
	if index < 0 {
		return 0, true, fmt.Errorf("negative array index %d is not supported", index)
	}
	return index, true, nil
}

func newStateContainer(nextPathPart string) any {
	if _, ok, _ := parseStateArrayIndex(nextPathPart); ok {
		return []any{}
	}
	return map[string]any{}
}

func cloneStateValue(value any) any {
	switch typedValue := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typedValue))
		for key, nestedValue := range typedValue {
			cloned[key] = cloneStateValue(nestedValue)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typedValue))
		for i, nestedValue := range typedValue {
			cloned[i] = cloneStateValue(nestedValue)
		}
		return cloned
	default:
		return value
	}
}
