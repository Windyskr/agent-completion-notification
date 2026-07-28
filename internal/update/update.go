// Package update 从 GitHub Release 下载并安装 acn 的最新版本。
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/Windyskr/agent-completion-notification/releases/latest"
	maxArchiveSize   = 100 << 20
	maxChecksumSize  = 1 << 20
	maxBinarySize    = 50 << 20
)

// Result 描述一次更新检查或安装的结果。
type Result struct {
	CurrentVersion string
	LatestVersion  string
	Executable     string
	Updated        bool
	CurrentIsNewer bool
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// Run 检查最新版本；checkOnly 为 false 时会下载并替换当前可执行文件。
func Run(ctx context.Context, currentVersion string, checkOnly bool) (Result, error) {
	result := Result{CurrentVersion: currentVersion}

	archiveExt, err := archiveExtension()
	if err != nil {
		return result, err
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	rel, err := fetchRelease(ctx, client)
	if err != nil {
		return result, err
	}
	result.LatestVersion = rel.TagName

	cmp, known := compareVersions(currentVersion, rel.TagName)
	if known && cmp >= 0 {
		result.CurrentIsNewer = cmp > 0
		return result, nil
	}
	if checkOnly {
		return result, nil
	}

	versionNumber := strings.TrimPrefix(rel.TagName, "v")
	archiveName := fmt.Sprintf("acn_%s_%s_%s%s", versionNumber, runtime.GOOS, runtime.GOARCH, archiveExt)
	archiveURL, ok := assetURL(rel, archiveName)
	if !ok {
		return result, fmt.Errorf("release %s 缺少 %s", rel.TagName, archiveName)
	}
	checksumURL, ok := assetURL(rel, "checksums.txt")
	if !ok {
		return result, fmt.Errorf("release %s 缺少 checksums.txt", rel.TagName)
	}

	archiveData, err := download(ctx, client, archiveURL, maxArchiveSize)
	if err != nil {
		return result, fmt.Errorf("下载 %s: %w", archiveName, err)
	}
	checksumData, err := download(ctx, client, checksumURL, maxChecksumSize)
	if err != nil {
		return result, fmt.Errorf("下载 checksums.txt: %w", err)
	}
	if err := verifyChecksum(archiveName, archiveData, checksumData); err != nil {
		return result, err
	}

	binaryName := "acn"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binaryData, err := extractBinary(archiveData, archiveExt, binaryName)
	if err != nil {
		return result, fmt.Errorf("解压 %s: %w", archiveName, err)
	}

	exe, err := os.Executable()
	if err != nil {
		return result, fmt.Errorf("获取自身路径: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return result, fmt.Errorf("解析自身路径: %w", err)
	}
	if info, statErr := os.Lstat(exe); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return result, fmt.Errorf("解析可执行文件软链接: %w", err)
		}
	}
	result.Executable = exe

	if err := replaceExecutable(exe, binaryData); err != nil {
		return result, err
	}
	result.Updated = true
	return result, nil
}

func archiveExtension() (string, error) {
	switch {
	case runtime.GOOS == "windows" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"):
		return ".zip", nil
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return ".tar.gz", nil
	default:
		return "", fmt.Errorf("暂不支持自动更新 %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func fetchRelease(ctx context.Context, client *http.Client) (release, error) {
	var rel release
	data, err := download(ctx, client, latestReleaseURL, maxChecksumSize)
	if err != nil {
		return rel, fmt.Errorf("查询最新版本: %w", err)
	}
	if err := json.Unmarshal(data, &rel); err != nil {
		return rel, fmt.Errorf("解析最新版本响应: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return rel, errors.New("最新版本响应缺少 tag_name")
	}
	return rel, nil
}

func download(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "acn-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("响应过大（%d bytes）", resp.ContentLength)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("响应超过 %d bytes", limit)
	}
	return data, nil
}

func assetURL(rel release, name string) (string, bool) {
	for _, asset := range rel.Assets {
		if asset.Name == name && asset.BrowserDownloadURL != "" {
			return asset.BrowserDownloadURL, true
		}
	}
	return "", false
}

func verifyChecksum(name string, archiveData, checksumData []byte) error {
	var expected string
	for _, line := range strings.Split(string(checksumData), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[len(fields)-1], "*") != name {
			continue
		}
		expected = strings.ToLower(fields[0])
		break
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("checksums.txt 中找不到 %s 的有效 SHA-256", name)
	}
	actualBytes := sha256.Sum256(archiveData)
	actual := hex.EncodeToString(actualBytes[:])
	if actual != expected {
		return fmt.Errorf("SHA-256 校验失败：期望 %s，实际 %s", expected, actual)
	}
	return nil
}

func extractBinary(archiveData []byte, extension, binaryName string) ([]byte, error) {
	switch extension {
	case ".zip":
		reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
		if err != nil {
			return nil, err
		}
		for _, file := range reader.File {
			if file.FileInfo().IsDir() || filepath.Base(filepath.ToSlash(file.Name)) != binaryName {
				continue
			}
			if file.UncompressedSize64 > maxBinarySize {
				return nil, errors.New("归档中的可执行文件过大")
			}
			src, err := file.Open()
			if err != nil {
				return nil, err
			}
			data, readErr := readLimited(src, maxBinarySize)
			closeErr := src.Close()
			if readErr != nil {
				return nil, readErr
			}
			if closeErr != nil {
				return nil, closeErr
			}
			return data, nil
		}
	case ".tar.gz":
		gz, err := gzip.NewReader(bytes.NewReader(archiveData))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			header, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if header.Typeflag != tar.TypeReg || filepath.Base(filepath.ToSlash(header.Name)) != binaryName {
				continue
			}
			if header.Size < 0 || header.Size > maxBinarySize {
				return nil, errors.New("归档中的可执行文件过大")
			}
			return readLimited(tr, maxBinarySize)
		}
	default:
		return nil, fmt.Errorf("未知归档格式 %s", extension)
	}
	return nil, fmt.Errorf("归档中找不到 %s", binaryName)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("文件超过 %d bytes", limit)
	}
	return data, nil
}

func replaceExecutable(exe string, binaryData []byte) error {
	info, err := os.Stat(exe)
	if err != nil {
		return fmt.Errorf("读取当前可执行文件: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(exe), ".acn-update-*")
	if err != nil {
		return fmt.Errorf("在安装目录创建临时文件: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if _, err := temp.Write(binaryData); err != nil {
		temp.Close()
		return fmt.Errorf("写入新版本: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("同步新版本: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭新版本: %w", err)
	}
	if err := os.Chmod(tempPath, info.Mode().Perm()); err != nil {
		return fmt.Errorf("设置新版本权限: %w", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Rename(tempPath, exe); err != nil {
			return fmt.Errorf("替换当前可执行文件: %w", err)
		}
		return nil
	}

	backupPath := exe + ".old"
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理上次更新备份 %s: %w", backupPath, err)
	}
	if err := os.Rename(exe, backupPath); err != nil {
		return fmt.Errorf("备份当前可执行文件: %w", err)
	}
	if err := os.Rename(tempPath, exe); err != nil {
		if rollbackErr := os.Rename(backupPath, exe); rollbackErr != nil {
			return fmt.Errorf("安装新版本失败: %v；回滚也失败: %w", err, rollbackErr)
		}
		return fmt.Errorf("安装新版本失败，已回滚: %w", err)
	}
	return nil
}

func compareVersions(current, latest string) (int, bool) {
	a, okA := parseVersion(current)
	b, okB := parseVersion(latest)
	if !okA || !okB {
		return 0, false
	}
	for i := range a {
		if a[i] < b[i] {
			return -1, true
		}
		if a[i] > b[i] {
			return 1, true
		}
	}
	return 0, true
}

func parseVersion(value string) ([3]int, bool) {
	var result [3]int
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "-", 2)[0]
	parts := strings.Split(value, ".")
	if len(parts) != len(result) {
		return result, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return result, false
		}
		result[i] = n
	}
	return result, true
}
