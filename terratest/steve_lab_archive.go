package test

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (p *localControlPanel) downloadSteveSourceArchive(record *steveLabRunRecord) error {
	if err := os.RemoveAll(record.SourceDir); err != nil {
		return fmt.Errorf("failed to reset Steve source directory: %w", err)
	}
	if err := os.MkdirAll(record.SourceDir, 0o755); err != nil {
		return fmt.Errorf("failed to create Steve source directory: %w", err)
	}

	archiveURL := "https://api.github.com/repos/rancher/steve/tarball/" + url.PathEscape(record.SteveRef)
	p.appendOperationOutput(panelOperationSteveLab, "[steve-lab] Downloading Steve source archive for "+record.SteveRef)
	req, err := http.NewRequest(http.MethodGet, archiveURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "rancher-runway")
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download Steve source archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download Steve source archive for %q: GitHub returned %s", record.SteveRef, resp.Status)
	}
	if err := extractGitHubTarball(resp.Body, record.SourceDir); err != nil {
		return err
	}
	return nil
}

func resolveSteveGitHubCommit(ref string) (string, error) {
	commitURL := "https://api.github.com/repos/rancher/steve/commits/" + url.PathEscape(ref)
	req, err := http.NewRequest(http.MethodGet, commitURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "rancher-runway")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var payload struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.SHA), nil
}

func extractGitHubTarball(reader io.Reader, destDir string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("failed to read Steve source archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to unpack Steve source archive: %w", err)
		}
		target, ok := githubTarballTarget(destDir, header.Name)
		if !ok {
			continue
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode) & 0o777
			if mode == 0 {
				mode = 0o644
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(file, tarReader)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

func githubTarballTarget(destDir string, name string) (string, bool) {
	name = strings.TrimPrefix(filepath.ToSlash(name), "/")
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", false
	}
	relative := filepath.Clean(filepath.FromSlash(parts[1]))
	if relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", false
	}
	target := filepath.Join(destDir, relative)
	cleanDest := filepath.Clean(destDir)
	cleanTarget := filepath.Clean(target)
	if cleanTarget != cleanDest && !strings.HasPrefix(cleanTarget, cleanDest+string(filepath.Separator)) {
		return "", false
	}
	return target, true
}
