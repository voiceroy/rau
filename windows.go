//go:build windows

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"strings"
)

const FILE_NAME = "rust-analyzer.exe"

func getAsset(r GithubRelease, systemInfo OS) (Asset, error) {
	var arch, ops, abi string

	ops, abi = "windows", "msvc"
	switch systemInfo.Arch {
	case "amd64":
		{
			arch = "x86_64"
		}
	case "386":
		{
			arch = "i686"
		}
	case "arm64":
		{
			arch = "aarch64"
		}
	default:
		return Asset{}, fmt.Errorf("architecture %s is not supported yet", systemInfo.Arch)
	}

	asset, err := r.GetMatchingTripleAsset(arch, ops, abi)
	if err != nil {
		return Asset{}, err
	}

	return asset, nil
}

func uncompress(file *os.File, destinationFile *os.File) error {
	reader, err := zip.OpenReader(file.Name())
	if err != nil {
		return fmt.Errorf("gz error: %w", err)
	}
	defer reader.Close()

	fmt.Println("Extracting")
	var zipFile *zip.File
	for _, v := range reader.File {
		if strings.HasSuffix(v.Name, ".exe") {
			zipFile = v
		}
	}

	if zipFile == nil {
		return fmt.Errorf("no executable found in the zip file")
	}

	rc, err := zipFile.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := destinationFile.Truncate(0); err != nil {
		return err
	}

	_, err = io.Copy(destinationFile, rc)
	if err != nil {
		return fmt.Errorf("gz error: %w", err)
	}

	return nil
}
