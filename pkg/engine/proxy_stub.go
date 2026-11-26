//go:build !windows

package engine

import (
	"net/http"
	"net/url"
)

func detectPlatformSystemProxy(_ *http.Request) (*url.URL, error) {
	return nil, nil
}
