//go:build linux || darwin

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
)

const FILE_NAME = "rust-analyzer"

func getAsset(r GithubRelease, systemInfo OS) (Asset, error) {
	if systemInfo.Platform == "darwin" {
		systemInfo.Platform = "apple"
	}

	var arch, abi string
	switch systemInfo.Arch {
	case "amd64":
		{
			arch, abi = "x86_64", "gnu"
		}
	case "arm64":
		{
			arch, abi = "aarch64", "gnu"
		}
	case "arm":
		{
			arch, abi = "arm", "gnueabihf"
		}
	default:
		return Asset{}, fmt.Errorf("architecture %s is not supported yet", systemInfo.Arch)
	}

	if systemInfo.Platform == "apple" {
		abi = "darwin"
	}

	asset, err := r.GetMatchingTripleAsset(arch, systemInfo.Platform, abi)
	if err != nil {
		return Asset{}, err
	}

	return asset, nil
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
