//go:build linux || darwin

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func HandleInstall(r GithubRelease, installPath string, httpClient *http.Client) error {
	ARCH := getArch()
	binaryPath := (filepath.Join(*InstallPath, FILE_NAME_UNIX))

	var err error
	var asset Asset
	switch ARCH {
	case "amd64":
		{
			asset, err = r.GetMatchingTripleAsset("x86_64", "linux", "gnu")
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("architecture %s is not supported yet", ARCH)
	}

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

	extractedFile, err := os.CreateTemp(*InstallPath, FILE_NAME_UNIX)
	if err != nil {
		return fmt.Errorf("unable to create temp file: %w", err)
	}
	defer os.Remove(extractedFile.Name())

	if err := uncompress(tempFile, extractedFile); err != nil {
		return err
	}

	tempFile.Close()
	extractedFile.Close()

	if err := setExectuableUnix(extractedFile.Name()); err != nil {
		return err
	}

	if err := os.Rename(extractedFile.Name(), binaryPath); err != nil {
		return fmt.Errorf("cannot move new version to destination: %w", err)
	}

	fmt.Println("Installed at:", binaryPath)
	return nil
}

func setExectuableUnix(path string) error {
	fmt.Println("Setting executable permission")
	if err := os.Chmod(path, 0o755); err != nil {
		return fmt.Errorf("cannot set executable permission: %w", err)
	}

	return nil
}

func uncompress(file *os.File, destinationFile *os.File) error {
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("gz error: %w", err)
	}
	defer reader.Close()

	fmt.Println("Extracting")
	_, err = io.Copy(destinationFile, reader)
	if err != nil {
		return fmt.Errorf("gz error: %w", err)
	}

	return nil
}
