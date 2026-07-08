package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/johnnyipcom/tgdownloader/pkg/yadisk"
)

// downloadYandexDiskFromPeer downloads all Yandex Disk links from a peer's messages.
func (r *Root) downloadYandexDiskFromPeer(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	getFileOptions, err := opts.newGetAllFilesOptions()
	if err != nil {
		return err
	}

	links, err := r.client.LinkService.GetYandexDiskLinks(ctx, peer, getFileOptions...)
	if err != nil {
		return err
	}

	return r.downloadYandexDiskLinks(ctx, links, opts)
}

// downloadYandexDiskLinks orchestrates downloading multiple Yandex Disk links.
func (r *Root) downloadYandexDiskLinks(ctx context.Context, links <-chan telegram.ExternalLink, opts downloadOptions) error {
	outputDir := r.cfg.GetString("downloader.dir.output")
	httpClient := createYadiskHTTPClient()
	ydClient := yadisk.NewClient(httpClient)

	p := renderer.NewProgress()
	if opts.ps {
		p.EnablePS(ctx)
	}
	defer p.WaitAndStop(ctx)

	var firstErr error
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case externalLink, ok := <-links:
			if !ok {
				return firstErr
			}

			if err := r.downloadSingleYandexDiskLink(ctx, ydClient, outputDir, externalLink, opts, p); err != nil {
				r.log.Error(err, "failed to download yandex disk link", "link", externalLink.URL, "message_id", externalLink.MessageID)
				renderer.RenderError(err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
}

// downloadSingleYandexDiskLink downloads a single Yandex Disk resource (file or directory).
func (r *Root) downloadSingleYandexDiskLink(
	ctx context.Context,
	ydClient *yadisk.Client,
	outputDir string,
	externalLink telegram.ExternalLink,
	opts downloadOptions,
	p renderer.Progress,
) error {
	// Build target directory
	subdirs := yadisk.BuildSubdirectories(externalLink.Metadata, opts.hashtags)
	targetDir := outputDir
	for _, subdir := range subdirs {
		targetDir = filepath.Join(targetDir, subdir)
	}

	// Resolve the Yandex Disk resource
	resolveTracker := p.UnitsTracker(fmt.Sprintf("yadisk:msg:%d:resolve", externalLink.MessageID), 1)
	resource, err := ydClient.ResolvePublicResourceDownloads(ctx, externalLink.URL)
	if err != nil {
		resolveTracker.Fail()
		return fmt.Errorf("resolve yadisk resource for msg_id=%d link=%q: %w", externalLink.MessageID, externalLink.URL, err)
	}
	resolveTracker.Increment(1)
	resolveTracker.Done()

	if len(resource.Files) == 0 {
		tracker := p.UnitsTracker(fmt.Sprintf("yadisk:msg:%d", externalLink.MessageID), 1)
		tracker.Fail()
		return fmt.Errorf("yadisk resource has no files for msg_id=%d link=%q", externalLink.MessageID, externalLink.URL)
	}

	if resource.Type == "dir" {
		targetDir = filepath.Join(targetDir, resource.Name)
	}

	filteredFiles := make([]yadisk.PublicDownload, 0, len(resource.Files))
	for _, item := range resource.Files {
		if yadisk.IsSkippableYandexFileName(item.Name) {
			r.log.Info("skip yandex disk system file", "name", item.Name, "path", item.Path)
			continue
		}

		filteredFiles = append(filteredFiles, item)
	}

	if len(filteredFiles) == 0 {
		r.log.Info("yandex disk link has no downloadable files after filtering", "link", externalLink.URL, "message_id", externalLink.MessageID)
		return nil
	}

	tracker := p.UnitsTracker(fmt.Sprintf("yadisk:msg:%d", externalLink.MessageID), len(filteredFiles))

	// Handle dry-run mode
	if opts.dryRun {
		for _, item := range filteredFiles {
			dir := targetDir
			if strings.TrimSpace(item.RelativeDir) != "" {
				dir = filepath.Join(targetDir, filepath.FromSlash(item.RelativeDir))
			}
			r.log.Info("dry-run yandex disk file", "link", externalLink.URL, "dir", dir, "name", item.Name)
			tracker.Increment(1)
		}
		tracker.Done()
		return nil
	}

	// Prepare target directory
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		tracker.Fail()
		return fmt.Errorf("prepare target dir %q: %w", targetDir, err)
	}

	// Create downloader service
	dlOpts := yadisk.FileDownloadOptions{
		AllowRewrite: opts.rewrite,
		DryRun:       opts.dryRun,
	}
	downloader := yadisk.NewFileDownloader(ydClient, dlOpts)
	downloader.SetLogCallback(func(msg string, fields ...interface{}) {
		r.log.Info(msg, fields...)
	})
	downloader.SetErrorCallback(func(err error) {
		r.log.Error(err, "download error")
	})

	// Download each file
	for _, item := range filteredFiles {
		itemDir := targetDir
		if strings.TrimSpace(item.RelativeDir) != "" {
			itemDir = filepath.Join(targetDir, filepath.FromSlash(item.RelativeDir))
		}

		if err := os.MkdirAll(itemDir, 0755); err != nil {
			tracker.Fail()
			return fmt.Errorf("prepare item dir %q: %w", itemDir, err)
		}

		targetPath := filepath.Join(itemDir, item.Name)
		if _, err := os.Stat(targetPath); err == nil && !opts.rewrite {
			r.log.Info("skip existing yandex disk file", "file", targetPath)
			tracker.Increment(1)
			continue
		}

		// Create progress writer for this file
		fileSize := item.Size
		if fileSize <= 0 {
			fileSize = 0
		}
		bytesTracker := p.BytesTracker(nil, item.Name, fileSize)

		// Download the file
		downloaded, err := downloader.DownloadFile(ctx, externalLink.URL, item, targetPath, bytesTracker)
		if err != nil {
			bytesTracker.Fail()
			tracker.Fail()
			return fmt.Errorf("download file %q: %w", item.Name, err)
		}

		// Skip if the file was skipped (not an error)
		if downloaded == nil {
			tracker.Increment(1)
			continue
		}

		bytesTracker.Done()
		tracker.Increment(1)
	}

	tracker.Done()
	return nil
}

// createYadiskHTTPClient creates an HTTP client with proper timeouts and transport settings.
func createYadiskHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 45 * time.Second,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 12 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSHandshakeTimeout:   12 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       60 * time.Second,
		},
	}
}
