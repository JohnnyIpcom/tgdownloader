package yadisk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const defaultAPIBaseURL = "https://cloud-api.yandex.net"
const yadiskBaseURL = "https://disk.yandex.ru"

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

// VideoStream represents a single quality stream for a video on Yandex Disk.
type VideoStream struct {
	Dimension string // e.g. "720p", "480p", "360p", "240p", "adaptive"
	Width     int
	Height    int
	URL       string // HLS playlist URL (.m3u8)
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
				Name: fileName,
				Size: item.Size,
				Path: item.Path,
				// Always resolve direct URL lazily per-file via file path to avoid
				// stale/preview URLs from directory listing metadata.
				DirectURL:   "",
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

// ============================================================================
// NEW: GetVideoStreams - Bypass read_without_download via HLS API
// ============================================================================

var (
	reYadiskSk       = regexp.MustCompile(`"sk"\s*:\s*"([^"]+)"`)
	reYadiskRootHash = regexp.MustCompile(`"path"\s*:\s*"([A-Za-z0-9+/]+=+):/`)
)

// GetVideoStreams fetches HLS video stream URLs for a video file in a Yandex Disk
// public folder. It uses the undocumented get-video-streams browser API, which
// bypasses read_without_download restrictions.
func (c *Client) GetVideoStreams(ctx context.Context, publicURL string, itemPath string) ([]VideoStream, error) {
	publicURL = strings.TrimSpace(publicURL)
	itemPath = strings.TrimSpace(itemPath)
	if publicURL == "" || itemPath == "" {
		return nil, fmt.Errorf("yadisk GetVideoStreams: empty publicURL or itemPath")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create cookie jar: %w", err)
	}
	pageClient := &http.Client{
		Jar:     jar,
		Timeout: c.httpClient.Timeout,
	}

	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, publicURL, nil)
	if err != nil {
		return nil, err
	}
	pageReq.Header.Set("User-Agent", browserUserAgent)
	pageReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	pageReq.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")

	pageResp, err := pageClient.Do(pageReq)
	if err != nil {
		return nil, fmt.Errorf("fetch yadisk public page: %w", err)
	}
	defer pageResp.Body.Close()

	pageBody, err := io.ReadAll(io.LimitReader(pageResp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read yadisk public page: %w", err)
	}
	pageText := string(pageBody)

	skMatch := reYadiskSk.FindStringSubmatch(pageText)
	if len(skMatch) < 2 {
		return nil, fmt.Errorf("yadisk page: could not extract sk token")
	}
	sk := skMatch[1]

	rootHashMatch := reYadiskRootHash.FindStringSubmatch(pageText)
	if len(rootHashMatch) < 2 {
		return nil, fmt.Errorf("yadisk page: could not extract root hash")
	}
	rootHash := rootHashMatch[1]

	if !strings.HasPrefix(itemPath, "/") {
		itemPath = "/" + itemPath
	}
	compositeHash := rootHash + ":" + itemPath

	apiURL := yadiskBaseURL + "/public/api/get-video-streams"
	bodyJSON, err := json.Marshal(map[string]string{"hash": compositeHash, "sk": sk})
	if err != nil {
		return nil, err
	}
	bodyEncoded := url.QueryEscape(string(bodyJSON))

	apiReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewBufferString(bodyEncoded))
	if err != nil {
		return nil, err
	}
	apiReq.Header.Set("Content-Type", "text/plain")
	apiReq.Header.Set("X-Requested-With", "XMLHttpRequest")
	apiReq.Header.Set("User-Agent", browserUserAgent)
	apiReq.Header.Set("Referer", publicURL)
	apiReq.Header.Set("Accept", "*/*")

	apiResp, err := pageClient.Do(apiReq)
	if err != nil {
		return nil, fmt.Errorf("get-video-streams request: %w", err)
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		return nil, formatHTTPError("get-video-streams api", apiResp)
	}

	var result videoStreamsResponse
	if err := json.NewDecoder(apiResp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode get-video-streams response: %w", err)
	}
	if result.Error {
		return nil, fmt.Errorf("get-video-streams api returned error (statusCode=%d)", result.StatusCode)
	}

	streams := make([]VideoStream, 0, len(result.Data.Videos))
	for _, v := range result.Data.Videos {
		streams = append(streams, VideoStream{
			Dimension: v.Dimension,
			Width:     v.Size.Width,
			Height:    v.Size.Height,
			URL:       v.URL,
		})
	}
	return streams, nil
}

type videoStreamsResponse struct {
	Error      bool `json:"error"`
	StatusCode int  `json:"statusCode"`
	Data       struct {
		StreamID string             `json:"streamId"`
		Duration int64              `json:"duration"`
		Videos   []videoStreamEntry `json:"videos"`
	} `json:"data"`
}

type videoStreamEntry struct {
	Dimension string `json:"dimension"`
	Size      struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"size"`
	URL string `json:"url"`
}

// ChooseBestVideoStream returns the stream with the highest resolution.
func ChooseBestVideoStream(streams []VideoStream) *VideoStream {
	if len(streams) == 0 {
		return nil
	}
	best := &streams[0]
	for i := range streams[1:] {
		s := &streams[i+1]
		if s.Dimension == "adaptive" {
			continue
		}
		if best.Dimension == "adaptive" || s.Height > best.Height {
			best = s
		}
	}
	return best
}

// DownloadHLSStream downloads an HLS stream from a .m3u8 playlist URL and
// writes the concatenated MPEG-TS data to w.
func (c *Client) DownloadHLSStream(ctx context.Context, m3u8URL string, w io.Writer) error {
	segURLs, err := c.resolveHLSSegments(ctx, m3u8URL)
	if err != nil {
		return err
	}
	for _, segURL := range segURLs {
		if err := c.downloadSegment(ctx, segURL, w); err != nil {
			return fmt.Errorf("download HLS segment %s: %w", segURL, err)
		}
	}
	return nil
}

func (c *Client) resolveHLSSegments(ctx context.Context, m3u8URL string) ([]string, error) {
	playlist, baseURL, err := c.fetchM3U8(ctx, m3u8URL)
	if err != nil {
		return nil, err
	}

	if strings.Contains(playlist, "#EXT-X-STREAM-INF") {
		subURL, err := parseMasterPlaylist(playlist, baseURL)
		if err != nil {
			return nil, err
		}
		playlist, baseURL, err = c.fetchM3U8(ctx, subURL)
		if err != nil {
			return nil, err
		}
	}

	return parseMediaPlaylist(playlist, baseURL)
}

func (c *Client) fetchM3U8(ctx context.Context, m3u8URL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m3u8URL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Referer", yadiskBaseURL+"/")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch m3u8 %s: %w", m3u8URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", formatHTTPError("fetch m3u8", resp)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", "", err
	}

	parsedURL, err := url.Parse(m3u8URL)
	if err != nil {
		return "", "", err
	}
	parsedURL.RawQuery = ""
	lastSlash := strings.LastIndex(parsedURL.Path, "/")
	if lastSlash >= 0 {
		parsedURL.Path = parsedURL.Path[:lastSlash+1]
	}
	baseURL := parsedURL.String()

	return string(body), baseURL, nil
}

func parseMasterPlaylist(playlist, baseURL string) (string, error) {
	type entry struct {
		bandwidth int
		uri       string
	}

	var entries []entry
	var curBandwidth int

	scanner := bufio.NewScanner(strings.NewReader(playlist))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			curBandwidth = 0
			for _, part := range strings.Split(line, ",") {
				if strings.HasPrefix(part, "BANDWIDTH=") {
					fmt.Sscanf(strings.TrimPrefix(part, "BANDWIDTH="), "%d", &curBandwidth)
				}
			}
		} else if line != "" && !strings.HasPrefix(line, "#") && curBandwidth > 0 {
			uri := resolveURL(baseURL, line)
			entries = append(entries, entry{curBandwidth, uri})
			curBandwidth = 0
		}
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("no sub-playlists found in HLS master playlist")
	}

	best := entries[0]
	for _, e := range entries[1:] {
		if e.bandwidth > best.bandwidth {
			best = e
		}
	}
	return best.uri, nil
}

func parseMediaPlaylist(playlist, baseURL string) ([]string, error) {
	var segments []string
	scanner := bufio.NewScanner(strings.NewReader(playlist))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		segments = append(segments, resolveURL(baseURL, line))
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("no segments found in HLS media playlist")
	}
	return segments, nil
}

func resolveURL(baseURL, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return baseURL + ref
}

func (c *Client) downloadSegment(ctx context.Context, segURL string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return formatHTTPError("download segment", resp)
	}

	_, err = io.Copy(w, resp.Body)
	return err
}

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// ============================================================================
// ORIGINAL: Helper functions
// ============================================================================

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
	case "original":
		return 110
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
	size := resolveDownloadSize(file.Size, resp.ContentLength, offset)

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

func resolveDownloadSize(fileSize, contentLength, offset int64) int64 {
	if offset == 0 && contentLength > 0 {
		return contentLength
	}
	if fileSize > 0 {
		return fileSize
	}
	return contentLength
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
