package main

import (
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	REPO_OWNER         = "rust-lang"
	REPO               = "rust-analyzer"
	API_URL            = "https://api.github.com/repos/" + REPO_OWNER + "/" + REPO + "/releases/tags/"
	MANIFEST_FILE_NAME = "rust-analyzer.version"
)

const (
	GITHUB_API_VERSION = "2026-03-10"
)

var (
	Release     = flag.String("release", "nightly", "Release tag of the version you want to install")
	InstallPath = flag.String("path", "", "Path to where you want to install")
	Timeout     = flag.Duration("timeout", 30*time.Second, "Timeout in seconds")
	AuthToken   = flag.String("authtoken", "", "Auth token to bypass default ratelimit")
)

type OS struct {
	Platform, Arch string
}

type Manifest struct {
	CommitHash  string
	TagName     string
	PublishedAt time.Time
}

func (m *Manifest) Read(from string) error {
	file, err := os.Open(from)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("Manifest file doesn't exist. Will install to reconcile")
		return nil // We don't have a manifest, reconcile by installing
	} else if err != nil {
		return err
	}
	defer file.Close()

	return gob.NewDecoder(file).Decode(m)
}

func (m *Manifest) Write(to string) error {
	file, err := os.Create(to)
	if err != nil {
		return err
	}
	defer file.Close()

	return gob.NewEncoder(file).Encode(m)
}

type Version string

func (v *Version) UnmarshalJSON(data []byte) error {
	var fullString string
	if err := json.Unmarshal(data, &fullString); err != nil {
		return err
	}

	startIndex := strings.Index(fullString, "(`v")
	if startIndex == -1 {
		*v = ""
		return nil // nightly versions are handled differently in the main func
	}

	endIndex := strings.Index(fullString[startIndex:], "`)")
	if endIndex == -1 {
		*v = ""
		return nil // nightly versions are handled differently in the main func
	}

	versionString := fullString[startIndex+3 : startIndex+endIndex]
	*v = Version(versionString)

	return nil
}

type GithubRelease struct {
	TagName     string    `json:"tag_name"`
	CommitHash  string    `json:"target_commitish"`
	Version     Version   `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

func (r GithubRelease) String() string {
	return fmt.Sprintf("Release: %v\nAssets:\n%v\n", r.TagName, r.Assets)
}

func (r GithubRelease) GetMatchingTripleAsset(arch, os, abi string) (Asset, error) {
	for _, v := range r.Assets {
		if v.File.Arch == arch && v.File.OS == os && v.File.ABI == abi {
			return v, nil
		}
	}

	return Asset{}, fmt.Errorf("asset with triple not found")
}

type Asset struct {
	Size        int    `json:"size"`
	DownloadURL string `json:"browser_download_url"`
	Digest      string `json:"digest"`
	File        File   `json:"name"`
}

type File struct {
	Name   string
	Arch   string
	Vendor string
	OS     string
	ABI    string
}

func (f *File) UnmarshalJSON(data []byte) error {
	var fullString string
	if err := json.Unmarshal(data, &fullString); err != nil {
		return err
	}

	f.Name = fullString
	trimmed := strings.TrimPrefix(fullString, "rust-analyzer-")
	trimmed = strings.TrimSuffix(trimmed, ".gz")
	trimmed = strings.TrimSuffix(trimmed, ".zip")

	parts := strings.Split(trimmed, "-")
	if len(parts) >= 1 {
		f.Arch = parts[0]
	}
	if len(parts) >= 2 {
		f.Vendor = parts[1]
	}
	if len(parts) >= 3 {
		f.OS = parts[2]
	}
	if len(parts) >= 4 {
		f.ABI = parts[3]
	}

	return nil
}

func HandleInstall(r GithubRelease, systemInfo OS, installPath string, httpClient *http.Client) error {
	asset, err := getAsset(r, systemInfo)
	if err != nil {
		return err
	}

	binaryPath := (filepath.Join(*InstallPath, FILE_NAME))
	tempFile, err := os.CreateTemp(*InstallPath, asset.File.Name)
	if err != nil {
		return err
	}
	defer os.Remove(tempFile.Name())

	if err := downloadFile(asset.DownloadURL, tempFile, httpClient); err != nil {
		return err
	}

	if ok, err := verifySHA256Sum(tempFile, strings.TrimPrefix(asset.Digest, "sha256:")); err != nil {
		return fmt.Errorf("failed to verify czechsum: %w", err)
	} else if !ok {
		return fmt.Errorf("corrupt download")
	} else {
		fmt.Println("Verified czechsum")
	}

	extractedFile, err := os.CreateTemp(*InstallPath, FILE_NAME)
	if err != nil {
		return fmt.Errorf("unable to create temp file: %w", err)
	}
	defer os.Remove(extractedFile.Name())

	if err := uncompress(tempFile, extractedFile); err != nil {
		return err
	}

	tempFile.Close()
	extractedFile.Close()

	if err := setExectuable(extractedFile.Name()); err != nil {
		return err
	}

	if err := os.Rename(extractedFile.Name(), binaryPath); err != nil {
		return fmt.Errorf("cannot move new version to destination: %w", err)
	}

	fmt.Println("Installed at:", binaryPath)
	return nil
}

func verifySHA256Sum(file *os.File, sum string) (bool, error) {
	_, err := file.Seek(0, io.SeekStart)
	if err != nil {
		return false, fmt.Errorf("file seek error: %w", err)
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("copy error: %w", err)
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return false, fmt.Errorf("file seek error: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)) == sum, nil
}

func downloadFile(url string, file *os.File, client *http.Client) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("http error: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("http error: %w", err)
	}

	return nil
}

func setExectuable(path string) error {
	fmt.Println("Setting executable permission")
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("cannot set executable permission: %w", err)
	}

	return nil
}

func main() {
	flag.Parse()

	if info, err := os.Stat(*InstallPath); err != nil {
		fmt.Println("Path error:", err.Error())
		return
	} else if !info.IsDir() {
		fmt.Println("Path is not a directory")
		return
	}

	var m Manifest
	manifestPath := filepath.Join(*InstallPath, MANIFEST_FILE_NAME)
	if err := m.Read(manifestPath); err != nil {
		fmt.Println("Failed to read manifest:", err.Error())
		return
	}

	httpClient := &http.Client{Timeout: *Timeout}
	req, err := http.NewRequest("GET", API_URL+*Release, nil)
	if err != nil {
		fmt.Println("Error creating a new request:", err.Error())
		return
	}

	if len(*AuthToken) != 0 {
		req.Header.Add("Authorization", "Bearer "+*AuthToken)
	}
	req.Header.Add("X-GitHub-Api-Version", GITHUB_API_VERSION)

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Println("Error performing request:", err.Error())
		return
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 403:
		{
			fmt.Println("API ratelimit reached, try again after sometime or use an API key")
			return
		}
	case 404:
		{
			fmt.Println("Release not found")
			return
		}
	}

	var releaseResponse GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releaseResponse); err != nil {
		fmt.Println("Failed to decode json response:", err.Error())
		return
	}

	nightlyInstalled := *Release == "nightly" && !m.PublishedAt.Before(releaseResponse.PublishedAt)
	taggedInstalled := m.TagName == releaseResponse.TagName && m.CommitHash == releaseResponse.CommitHash

	if len(m.CommitHash) == 0 || len(m.TagName) == 0 || m.PublishedAt.IsZero() {
		// Manifest is invalid, install to reconcile
		nightlyInstalled = false
		taggedInstalled = false
	}

	if nightlyInstalled || taggedInstalled {
		fmt.Println("Already installed, skipping")
		return
	}

	if m.TagName == releaseResponse.TagName && m.CommitHash != releaseResponse.CommitHash {
		fmt.Println("Remote commit hash for the release", *Release, "differs, installing to match to the new commit hash")
	}

	systemInfo := OS{
		Platform: runtime.GOOS,
		Arch:     runtime.GOARCH,
	}

	if releaseResponse.TagName == *Release {
		if err := HandleInstall(releaseResponse, systemInfo, *InstallPath, httpClient); err != nil {
			fmt.Println("Install failed with error: ", err.Error())
		}

		m.CommitHash = releaseResponse.CommitHash
		m.TagName = releaseResponse.TagName
		m.PublishedAt = releaseResponse.PublishedAt
		if err := m.Write(manifestPath); err != nil {
			fmt.Println("Failed to write manifest file:", err.Error())
		}
	}
}
