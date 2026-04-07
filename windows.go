//go:build windows

package main

import (
	"net/http"
)

func HandleInstall(r GithubRelease, installPath string, httpClient *http.Client) error {
	panic("Not implemented")
}
