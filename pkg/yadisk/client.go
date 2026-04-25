package yadisk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultAPIBaseURL = "https://cloud-api.yandex.net"

type downloadResponse struct {
	Href string `json:"href"`
}

type publicResourceResponse struct {
	Type     string          `json:"type"`
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	File     string          `json:"file"`
	MimeType string          `json:"mime_type"`
	Preview  string          `json:"preview"`
	Sizes    []resourceSize  `json:"sizes"`
	Size     int64           `json:"size"`
	Embedded *publicEmbedded `json:"_embedded"`
}

type resourceSize struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Size string `json:"size"`
}

type publicEmbedded struct {
	Items  []publicResourceResponse `json:"items"`
	Limit  int                      `json:"limit"`
	Offset int                      `json:"offset"`
	Total  int                      `json:"total"`
}

type Client struct {
	httpClient *http.Client
	apiBaseURL string
}

type PublicFile struct {
	Name      string
	Size      int64
	Offset    int64
	DirectURL string
	Body      io.ReadCloser
}

type PublicDownload struct {
	Name        string
	Size        int64
	DirectURL   string
	Path        string
	RelativeDir string
}

type ResolvedPublicResource struct {
	Name  string
	Type  string
	Files []PublicDownload
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	return &Client{
		httpClient: httpClient,
		apiBaseURL: defaultAPIBaseURL,
	}
}

func (c *Client) ResolvePublicDownloadURL(ctx context.Context, publicURL string) (string, error) {
	href, err := c.resolveDownloadURL(ctx, publicURL, "")
	if err != nil {
		return "", err
	}
	if href != "" {
		return href, nil
	}

	meta, err := c.getPublicResource(ctx, publicURL, "", 0, 0)
	if err != nil {
		return "", err
	}
	if meta.Path != "" {
		href, err = c.resolveDownloadURL(ctx, publicURL, meta.Path)
		if err != nil {
			return "", err
		}
		if href != "" {
			return href, nil
		}
	}

	if fileURL := strings.TrimSpace(meta.File); fileURL != "" {
		return fileURL, nil
	}

	if canUseMediaFallback(*meta) {
		if sizeURL := chooseResourceSizeURL(*meta); sizeURL != "" {
			return sizeURL, nil
		}
		if previewURL := strings.TrimSpace(meta.Preview); previewURL != "" {
			return previewURL, nil
		}
	}

	if meta.Type == "dir" {
		return "", fmt.Errorf("yandex disk api returned empty href (public resource is a directory: %q)", meta.Name)
	}

	if err := c.detectReadWithoutDownload(ctx, publicURL); err != nil {
		return "", err
	}

	return "", fmt.Errorf("yandex disk api returned empty href")
}

func (c *Client) ResolvePublicResourceDownloads(ctx context.Context, publicURL string) (*ResolvedPublicResource, error) {
	meta, err := c.getPublicResource(ctx, publicURL, "", 0, 0)
	if err != nil {
		return nil, err
	}

	resourceName := sanitizeFilename(strings.TrimSpace(meta.Name))
	if resourceName == "" || resourceName == "download.bin" {
		resourceName = "yadisk"
	}

	if meta.Type == "dir" {
		files, err := c.collectDirectoryFiles(ctx, publicURL, meta.Path, meta.Path)
		if err != nil {
			return nil, err
		}

		return &ResolvedPublicResource{
			Name:  resourceName,
			Type:  "dir",
			Files: files,
		}, nil
	}

	directURL, err := c.resolveFileDirectURL(ctx, publicURL, *meta)
	if err != nil {
		return nil, err
	}

	fileName := sanitizeFilename(strings.TrimSpace(meta.Name))
	if fileName == "" || fileName == "download.bin" {
		fileName = "download.bin"
	}

	return &ResolvedPublicResource{
		Name: resourceName,
		Type: "file",
		Files: []PublicDownload{
			{
				Name:      fileName,
				Size:      meta.Size,
				Path:      meta.Path,
				DirectURL: directURL,
			},
		},
	}, nil
}

func (c *Client) resolveDownloadURL(ctx context.Context, publicURL, resourcePath string) (string, error) {
	endpoint := strings.TrimRight(c.apiBaseURL, "/") + "/v1/disk/public/resources/download?public_key=" + url.QueryEscape(publicURL)
	if strings.TrimSpace(resourcePath) != "" {
		endpoint += "&path=" + url.QueryEscape(resourcePath)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", formatHTTPError("unexpected yandex disk api status", resp)
	}

	var result downloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return strings.TrimSpace(result.Href), nil
}

func (c *Client) getPublicResource(ctx context.Context, publicURL, resourcePath string, limit, offset int) (*publicResourceResponse, error) {
	q := url.Values{}
	q.Set("public_key", publicURL)
	if strings.TrimSpace(resourcePath) != "" {
		q.Set("path", resourcePath)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
		q.Set("offset", fmt.Sprintf("%d", offset))
	}

	endpoint := strings.TrimRight(c.apiBaseURL, "/") + "/v1/disk/public/resources?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, formatHTTPError("unexpected yandex disk public resource status", resp)
	}

	var result publicResourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Client) collectDirectoryFiles(ctx context.Context, publicURL, rootPath, dirPath string) ([]PublicDownload, error) {
	items, err := c.listDirectoryItems(ctx, publicURL, dirPath)
	if err != nil {
		return nil, err
	}

	files := make([]PublicDownload, 0, len(items))
	for _, item := range items {
		switch item.Type {
		case "dir":
			nested, err := c.collectDirectoryFiles(ctx, publicURL, rootPath, item.Path)
			if err != nil {
				return nil, err
			}
			files = append(files, nested...)
		case "file":
			relPath := strings.TrimPrefix(item.Path, rootPath)
			relPath = strings.TrimPrefix(relPath, "/")
			relDir := path.Dir(relPath)
			if relDir == "." {
				relDir = ""
			}

			fileName := sanitizeFilename(strings.TrimSpace(item.Name))
			if fileName == "" || fileName == "download.bin" {
				fileName = sanitizeFilename(path.Base(item.Path))
			}

			files = append(files, PublicDownload{
				Name:        fileName,
				Size:        item.Size,
				Path:        item.Path,
				DirectURL:   directURLFromResourceMeta(item),
				RelativeDir: relDir,
			})
		}
	}

	return files, nil
}

func (c *Client) listDirectoryItems(ctx context.Context, publicURL, dirPath string) ([]publicResourceResponse, error) {
	const pageSize = 1000

	offset := 0
	var items []publicResourceResponse
	for {
		meta, err := c.getPublicResource(ctx, publicURL, dirPath, pageSize, offset)
		if err != nil {
			return nil, err
		}

		if meta.Embedded == nil || len(meta.Embedded.Items) == 0 {
			break
		}

		items = append(items, meta.Embedded.Items...)
		offset += len(meta.Embedded.Items)
		if offset >= meta.Embedded.Total {
			break
		}
	}

	return items, nil
}

func (c *Client) resolveFileDirectURL(ctx context.Context, publicURL string, meta publicResourceResponse) (string, error) {
	if meta.Path != "" {
		href, err := c.resolveDownloadURL(ctx, publicURL, meta.Path)
		if err != nil {
			return "", err
		}
		if href != "" {
			return href, nil
		}
	}

	href, err := c.resolveDownloadURL(ctx, publicURL, "")
	if err != nil {
		return "", err
	}
	if href != "" {
		return href, nil
	}

	if fileURL := strings.TrimSpace(meta.File); fileURL != "" {
		return fileURL, nil
	}

	if meta.Path != "" {

		fileMeta, err := c.getPublicResource(ctx, publicURL, meta.Path, 0, 0)
		if err != nil {
			return "", err
		}
		if fileURL := strings.TrimSpace(fileMeta.File); fileURL != "" {
			return fileURL, nil
		}
		if canUseMediaFallback(*fileMeta) {
			if sizeURL := chooseResourceSizeURL(*fileMeta); sizeURL != "" {
				return sizeURL, nil
			}
			if previewURL := strings.TrimSpace(fileMeta.Preview); previewURL != "" {
				return previewURL, nil
			}
		}
	}

	if canUseMediaFallback(meta) {
		if sizeURL := chooseResourceSizeURL(meta); sizeURL != "" {
			return sizeURL, nil
		}

		if previewURL := strings.TrimSpace(meta.Preview); previewURL != "" {
			return previewURL, nil
		}
	}

	if meta.Type == "dir" {
		return "", fmt.Errorf("yandex disk api returned empty href (public resource is a directory: %q)", meta.Name)
	}

	if err := c.detectReadWithoutDownload(ctx, publicURL); err != nil {
		return "", err
	}

	return "", fmt.Errorf("yandex disk api returned empty href (type=%q name=%q path=%q)", meta.Type, meta.Name, meta.Path)
}

func (c *Client) detectReadWithoutDownload(ctx context.Context, publicURL string) error {
	trimmed := strings.TrimSpace(publicURL)
	if trimmed == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimmed, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}

	// 2 MiB is enough to inspect bootstrap state without loading huge pages.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil
	}
	text := strings.ToLower(string(body))

	if strings.Contains(text, "read_without_download") || strings.Contains(text, "read_with_password_without_download") {
		return fmt.Errorf("yandex disk public link forbids file download (rights include read_without_download)")
	}

	return nil
}

func (c *Client) ResolvePublicFileDownloadURL(ctx context.Context, publicURL string, file PublicDownload) (string, error) {
	if directURL := strings.TrimSpace(file.DirectURL); directURL != "" {
		return directURL, nil
	}

	meta, err := c.getPublicResource(ctx, publicURL, file.Path, 0, 0)
	if err != nil {
		return "", err
	}

	return c.resolveFileDirectURL(ctx, publicURL, *meta)
}

func directURLFromResourceMeta(meta publicResourceResponse) string {
	if fileURL := strings.TrimSpace(meta.File); fileURL != "" {
		return fileURL
	}
	if !canUseMediaFallback(meta) {
		return ""
	}
	if sizeURL := chooseResourceSizeURL(meta); sizeURL != "" {
		return sizeURL
	}
	return strings.TrimSpace(meta.Preview)
}

func canUseMediaFallback(meta publicResourceResponse) bool {
	mimeType := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if strings.HasPrefix(mimeType, "image/") {
		return true
	}
	if strings.HasPrefix(mimeType, "video/") {
		return false
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(meta.Name)))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".heic", ".avif":
		return true
	default:
		return false
	}
}

func chooseResourceSizeURL(meta publicResourceResponse) string {
	bestURL := ""
	bestRank := -1
	for _, s := range meta.Sizes {
		u := strings.TrimSpace(s.URL)
		if u == "" {
			continue
		}
		r := sizeRank(s.Name)
		if r > bestRank {
			bestRank = r
			bestURL = u
		}
	}
	if bestURL != "" {
		return bestURL
	}

	return ""
}

func sizeRank(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "orig":
		return 100
	case "xxxl":
		return 90
	case "xxl":
		return 80
	case "xl":
		return 70
	case "x":
		return 60
	case "l":
		return 50
	case "m":
		return 40
	case "s":
		return 30
	case "xs":
		return 20
	default:
		return 10
	}
}

func (c *Client) OpenPublicFile(ctx context.Context, publicURL string) (*PublicFile, error) {
	resource, err := c.ResolvePublicResourceDownloads(ctx, publicURL)
	if err != nil {
		return nil, err
	}
	if len(resource.Files) == 0 {
		return nil, fmt.Errorf("yandex disk resource has no files")
	}
	if resource.Type == "dir" {
		return nil, fmt.Errorf("yandex disk public resource is a directory: %q", resource.Name)
	}

	return c.OpenDirectFile(ctx, resource.Files[0])
}

func (c *Client) OpenDirectFile(ctx context.Context, file PublicDownload) (*PublicFile, error) {
	return c.openDirectFileAt(ctx, file, 0)
}

func (c *Client) OpenDirectFileRange(ctx context.Context, file PublicDownload, offset int64) (*PublicFile, error) {
	if offset < 0 {
		return nil, fmt.Errorf("invalid offset %d", offset)
	}

	return c.openDirectFileAt(ctx, file, offset)
}

func (c *Client) openDirectFileAt(ctx context.Context, file PublicDownload, offset int64) (*PublicFile, error) {
	directURL := strings.TrimSpace(file.DirectURL)
	if directURL == "" {
		return nil, fmt.Errorf("empty direct url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, directURL, nil)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if offset == 0 && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		defer resp.Body.Close()
		return nil, formatHTTPError("unexpected yandex disk download status", resp)
	}
	if offset > 0 && resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, formatHTTPError("unexpected yandex disk ranged download status", resp)
	}

	name := FilenameFromResponse(resp, file.Name)
	size := file.Size
	if size <= 0 {
		size = resp.ContentLength
	}

	actualOffset := int64(0)
	if offset > 0 && resp.StatusCode == http.StatusPartialContent {
		actualOffset = offset
	}

	return &PublicFile{
		Name:      name,
		Size:      size,
		Offset:    actualOffset,
		DirectURL: directURL,
		Body:      resp.Body,
	}, nil
}

func FilenameFromResponse(resp *http.Response, fallback string) string {
	if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
		_, params, err := mime.ParseMediaType(disposition)
		if err == nil {
			if filename := strings.TrimSpace(params["filename"]); filename != "" {
				return sanitizeFilename(filename)
			}
		}
	}

	if resp.Request != nil && resp.Request.URL != nil {
		if name := path.Base(resp.Request.URL.Path); name != "" && name != "/" && name != "." {
			return sanitizeFilename(name)
		}
	}

	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = "download.bin"
	}

	return sanitizeFilename(fallback)
}

func sanitizeFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "download.bin"
	}

	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	return filename
}

func formatHTTPError(prefix string, resp *http.Response) error {
	body := ""
	if resp != nil && resp.Body != nil {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		body = strings.TrimSpace(string(data))
	}

	if body == "" {
		return fmt.Errorf("%s: %s", prefix, resp.Status)
	}

	return fmt.Errorf("%s: %s: %s", prefix, resp.Status, body)
}
