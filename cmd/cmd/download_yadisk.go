package cmd

import (
	"context"
	"fmt"
	"io"
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

func (r *Root) downloadYandexDiskLinks(ctx context.Context, links <-chan telegram.ExternalLink, opts downloadOptions) error {
	outputDir := r.cfg.GetString("downloader.dir.output")
	httpClient := &http.Client{
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

func (r *Root) downloadSingleYandexDiskLink(
	ctx context.Context,
	ydClient *yadisk.Client,
	outputDir string,
	externalLink telegram.ExternalLink,
	opts downloadOptions,
	p renderer.Progress,
) error {
	subdirs := yandexDiskSubdirs(externalLink.Metadata, opts.hashtags)
	targetDir := outputDir
	for _, subdir := range subdirs {
		targetDir = filepath.Join(targetDir, subdir)
	}

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

	tracker := p.UnitsTracker(fmt.Sprintf("yadisk:msg:%d", externalLink.MessageID), len(resource.Files))

	if opts.dryRun {
		for _, item := range resource.Files {
			dir := targetDir
			if strings.TrimSpace(item.RelativeDir) != "" {
				dir = filepath.Join(targetDir, filepath.FromSlash(item.RelativeDir))
			}

			r.log.Info("dry-run yandex disk file", "link", externalLink.URL, "direct_url", item.DirectURL, "dir", dir, "name", item.Name)
			tracker.Increment(1)
		}
		tracker.Done()
		return nil
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		tracker.Fail()
		return fmt.Errorf("prepare target dir %q for msg_id=%d: %w", targetDir, externalLink.MessageID, err)
	}

	for _, item := range resource.Files {
		itemDir := targetDir
		if strings.TrimSpace(item.RelativeDir) != "" {
			itemDir = filepath.Join(targetDir, filepath.FromSlash(item.RelativeDir))
		}

		if err := os.MkdirAll(itemDir, 0755); err != nil {
			tracker.Fail()
			return fmt.Errorf("prepare target dir %q for msg_id=%d: %w", itemDir, externalLink.MessageID, err)
		}

		targetPath := filepath.Join(itemDir, item.Name)
		if _, err := os.Stat(targetPath); err == nil && !opts.rewrite {
			r.log.Info("skip existing yandex disk file", "file", targetPath)
			tracker.Increment(1)
			continue
		}

		if strings.TrimSpace(item.DirectURL) == "" {
			directURL, err := ydClient.ResolvePublicFileDownloadURL(ctx, externalLink.URL, item)
			if err != nil {
				if isSkippableYandexItem(item.Name, err) {
					r.log.Info("skip non-critical yadisk item", "item", item.Name, "path", item.Path, "reason", err.Error())
					tracker.Increment(1)
					continue
				}

				tracker.Fail()
				return fmt.Errorf("resolve yadisk file url for msg_id=%d link=%q item=%q path=%q: %w", externalLink.MessageID, externalLink.URL, item.Name, item.Path, err)
			}
			item.DirectURL = directURL
		}

		publicFile, err := ydClient.OpenDirectFile(ctx, item)
		if err != nil {
			tracker.Fail()
			return fmt.Errorf("open yadisk file for msg_id=%d link=%q item=%q: %w", externalLink.MessageID, externalLink.URL, item.Name, err)
		}

		file, err := os.Create(targetPath)
		if err != nil {
			_ = publicFile.Body.Close()
			tracker.Fail()
			return fmt.Errorf("create target file %q for msg_id=%d: %w", targetPath, externalLink.MessageID, err)
		}

		total := publicFile.Size
		if total < 0 {
			total = 0
		}

		bytesTracker := p.BytesTracker(file, publicFile.Name, total)
		written, copyErr := io.Copy(bytesTracker, publicFile.Body)
		closeErr := publicFile.Body.Close()
		if copyErr != nil {
			bytesTracker.Fail()
			tracker.Fail()
			_ = file.Close()
			_ = os.Remove(targetPath)
			return fmt.Errorf("download yadisk body for msg_id=%d link=%q into %q: %w", externalLink.MessageID, externalLink.URL, targetPath, copyErr)
		}
		if closeErr != nil {
			bytesTracker.Fail()
			tracker.Fail()
			_ = file.Close()
			_ = os.Remove(targetPath)
			return fmt.Errorf("close yadisk response body for msg_id=%d link=%q into %q: %w", externalLink.MessageID, externalLink.URL, targetPath, closeErr)
		}

		if total > 0 && written < total {
			const maxRangeParts = 256
			for part := 0; written < total && part < maxRangeParts; part++ {
				rangeFile, err := ydClient.OpenDirectFileRange(ctx, item, written)
				if err != nil {
					bytesTracker.Fail()
					tracker.Fail()
					_ = file.Close()
					_ = os.Remove(targetPath)
					return fmt.Errorf("resume yadisk download for msg_id=%d link=%q item=%q offset=%d: %w", externalLink.MessageID, externalLink.URL, item.Name, written, err)
				}

				if rangeFile.Offset == 0 && written > 0 {
					// Server ignored Range and returned full body from the beginning.
					if err := file.Truncate(0); err != nil {
						_ = rangeFile.Body.Close()
						bytesTracker.Fail()
						tracker.Fail()
						_ = file.Close()
						_ = os.Remove(targetPath)
						return fmt.Errorf("truncate file before full restart for msg_id=%d link=%q item=%q: %w", externalLink.MessageID, externalLink.URL, item.Name, err)
					}
					if _, err := file.Seek(0, 0); err != nil {
						_ = rangeFile.Body.Close()
						bytesTracker.Fail()
						tracker.Fail()
						_ = file.Close()
						_ = os.Remove(targetPath)
						return fmt.Errorf("seek file before full restart for msg_id=%d link=%q item=%q: %w", externalLink.MessageID, externalLink.URL, item.Name, err)
					}

					rewritten, restartErr := io.Copy(file, rangeFile.Body)
					rangeCloseErr := rangeFile.Body.Close()
					if restartErr != nil {
						bytesTracker.Fail()
						tracker.Fail()
						_ = file.Close()
						_ = os.Remove(targetPath)
						return fmt.Errorf("restart full yadisk copy for msg_id=%d link=%q item=%q: %w", externalLink.MessageID, externalLink.URL, item.Name, restartErr)
					}
					if rangeCloseErr != nil {
						bytesTracker.Fail()
						tracker.Fail()
						_ = file.Close()
						_ = os.Remove(targetPath)
						return fmt.Errorf("close restarted yadisk response body for msg_id=%d link=%q item=%q: %w", externalLink.MessageID, externalLink.URL, item.Name, rangeCloseErr)
					}
					if rewritten <= 0 {
						bytesTracker.Fail()
						tracker.Fail()
						_ = file.Close()
						_ = os.Remove(targetPath)
						return fmt.Errorf("restart full yadisk copy made no progress for msg_id=%d link=%q item=%q", externalLink.MessageID, externalLink.URL, item.Name)
					}

					written = rewritten
					continue
				}

				chunkWritten, chunkErr := io.Copy(bytesTracker, rangeFile.Body)
				rangeCloseErr := rangeFile.Body.Close()
				if chunkErr != nil {
					bytesTracker.Fail()
					tracker.Fail()
					_ = file.Close()
					_ = os.Remove(targetPath)
					return fmt.Errorf("resume copy yadisk body for msg_id=%d link=%q item=%q offset=%d: %w", externalLink.MessageID, externalLink.URL, item.Name, written, chunkErr)
				}
				if rangeCloseErr != nil {
					bytesTracker.Fail()
					tracker.Fail()
					_ = file.Close()
					_ = os.Remove(targetPath)
					return fmt.Errorf("close resumed yadisk response body for msg_id=%d link=%q item=%q offset=%d: %w", externalLink.MessageID, externalLink.URL, item.Name, written, rangeCloseErr)
				}
				if chunkWritten <= 0 {
					bytesTracker.Fail()
					tracker.Fail()
					_ = file.Close()
					_ = os.Remove(targetPath)
					return fmt.Errorf("resume made no progress for msg_id=%d link=%q item=%q offset=%d", externalLink.MessageID, externalLink.URL, item.Name, written)
				}

				written += chunkWritten
			}

			if written < total {
				bytesTracker.Fail()
				tracker.Fail()
				_ = file.Close()
				_ = os.Remove(targetPath)
				return fmt.Errorf("incomplete yadisk download for msg_id=%d link=%q item=%q: got %d bytes of %d", externalLink.MessageID, externalLink.URL, item.Name, written, total)
			}
		}

		fileCloseErr := file.Close()
		if fileCloseErr != nil {
			bytesTracker.Fail()
			tracker.Fail()
			_ = os.Remove(targetPath)
			return fmt.Errorf("close target file %q for msg_id=%d: %w", targetPath, externalLink.MessageID, fileCloseErr)
		}
		bytesTracker.Done()

		tracker.Increment(1)
	}

	tracker.Done()
	return nil
}

func yandexDiskSubdirs(metadata map[string]interface{}, saveByHashtags bool) []string {
	if metadata == nil {
		return nil
	}

	var subdirs []string
	if peerName, ok := metadata["peername"].(string); ok && peerName != "" {
		subdirs = append(subdirs, peerName)
	}

	if saveByHashtags {
		if hashtags, ok := metadata["hashtags"].([]string); ok {
			subdirs = append(subdirs, hashtags...)
		}
	}

	return subdirs
}

func isSkippableYandexItem(name string, err error) bool {
	if err == nil {
		return false
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "empty href") {
		return false
	}

	fileName := strings.ToLower(strings.TrimSpace(name))
	switch fileName {
	case "thumbs.db", ".ds_store", "desktop.ini":
		return true
	default:
		return false
	}
}
