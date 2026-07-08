package yadisk

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

// FileDownloadOptions configures file download behavior.
type FileDownloadOptions struct {
	// AllowRewrite allows overwriting existing files
	AllowRewrite bool
	// DryRun performs no actual download, only logs
	DryRun bool
}

// DownloadedFile holds information about a successfully downloaded file.
type DownloadedFile struct {
	Path        string
	Size        int64
	IsHLSStream bool // true if downloaded via HLS fallback
}

// FileDownloader orchestrates file downloads with resume support.
type FileDownloader struct {
	client  *Client
	opts    FileDownloadOptions
	onError func(error)                                 // optional error callback
	onLog   func(message string, fields ...interface{}) // optional logging callback
}

// NewFileDownloader creates a new file downloader.
func NewFileDownloader(client *Client, opts FileDownloadOptions) *FileDownloader {
	return &FileDownloader{
		client: client,
		opts:   opts,
	}
}

// SetErrorCallback sets an error callback for logging.
func (fd *FileDownloader) SetErrorCallback(fn func(error)) {
	fd.onError = fn
}

// SetLogCallback sets a logging callback.
func (fd *FileDownloader) SetLogCallback(fn func(message string, fields ...interface{})) {
	fd.onLog = fn
}

// DownloadFile downloads a single file to the specified path.
// publicURL is the Yandex Disk public link.
// file is the file to download.
// targetPath is the destination file path.
// Returns information about the downloaded file or an error.
func (fd *FileDownloader) DownloadFile(
	ctx context.Context,
	publicURL string,
	file PublicDownload,
	targetPath string,
	progressWriter io.Writer, // for progress tracking
) (*DownloadedFile, error) {
	if fd.opts.DryRun {
		fd.log("dry-run: would download", "file", file.Name, "path", targetPath)
		return &DownloadedFile{Path: targetPath}, nil
	}

	// Check if file already exists
	if _, err := os.Stat(targetPath); err == nil && !fd.opts.AllowRewrite {
		fd.log("skipping existing file", "file", targetPath)
		return &DownloadedFile{Path: targetPath}, nil
	}

	directURL := strings.TrimSpace(file.DirectURL)
	if directURL == "" {
		var err error
		directURL, err = fd.client.ResolvePublicFileDownloadURL(ctx, publicURL, file)
		if err != nil {
			// Try HLS fallback for videos when direct URL is restricted or unavailable.
			if ShouldUseHLSFallback(file.Name, err) {
				fd.log("attempting HLS fallback", "file", file.Name)
				downloaded, hlsErr := fd.downloadViaHLS(ctx, publicURL, file, targetPath, progressWriter)
				if hlsErr == nil {
					fd.log("successfully downloaded via HLS", "file", file.Name)
					return downloaded, nil
				}
				if fd.onError != nil {
					fd.onError(fmt.Errorf("HLS fallback failed for %s: %w", file.Name, hlsErr))
				}
			}

			// Check if error is skippable
			if IsSkippableYandexItem(file.Name, err) {
				fd.log("skipping non-critical item", "file", file.Name, "reason", err.Error())
				return nil, nil // nil file indicates skip, not error
			}

			return nil, apperr.New("yadisk.downloader.resolve_download_url", apperr.KindNetwork, fmt.Errorf("resolve download url for %q: %w", file.Name, err))
		}
	}

	file.DirectURL = directURL

	// Download via direct URL
	downloaded, err := fd.downloadViaDirect(ctx, file, targetPath, progressWriter)
	if err == nil {
		return downloaded, nil
	}

	if strings.TrimSpace(file.Path) == "" {
		return nil, err
	}

	fd.log("retrying direct download with refreshed URL", "file", file.Name, "path", file.Path)
	refreshedURL, refreshErr := fd.client.ResolvePublicFileDownloadURL(ctx, publicURL, PublicDownload{
		Name: file.Name,
		Path: file.Path,
		Size: file.Size,
	})
	if refreshErr != nil {
		return nil, err
	}

	file.DirectURL = strings.TrimSpace(refreshedURL)
	if file.DirectURL == "" {
		return nil, err
	}

	return fd.downloadViaDirect(ctx, file, targetPath, progressWriter)
}

// downloadViaDirect downloads a file from a direct URL with resume support.
func (fd *FileDownloader) downloadViaDirect(
	ctx context.Context,
	file PublicDownload,
	targetPath string,
	progressWriter io.Writer,
) (*DownloadedFile, error) {
	publicFile, err := fd.client.OpenDirectFile(ctx, file)
	if err != nil {
		return nil, apperr.New("yadisk.downloader.open_direct_file", apperr.KindNetwork, fmt.Errorf("open file %q: %w", file.Name, err))
	}
	defer publicFile.Body.Close()

	// Create target file
	out, err := os.Create(targetPath)
	if err != nil {
		return nil, apperr.New("yadisk.downloader.create_target_file", apperr.KindIO, fmt.Errorf("create file %q: %w", targetPath, err))
	}

	total := publicFile.Size
	if total < 0 {
		total = 0
	}

	// Download with progress tracking
	var writer io.Writer = out
	if progressWriter != nil {
		writer = io.MultiWriter(out, progressWriter)
	}
	written, copyErr := io.Copy(writer, publicFile.Body)
	if copyErr != nil {
		out.Close()
		os.Remove(targetPath)
		return nil, apperr.New("yadisk.downloader.copy_body", apperr.KindNetwork, fmt.Errorf("download body: %w", copyErr))
	}

	// Handle resume if not all bytes were downloaded
	if total > 0 && written < total {
		written, err = fd.resumeDownload(ctx, file, out, written, total, progressWriter)
		if err != nil {
			out.Close()
			os.Remove(targetPath)
			return nil, apperr.Wrap("yadisk.downloader.resume_download", err)
		}
	}

	if err := out.Close(); err != nil {
		os.Remove(targetPath)
		return nil, apperr.New("yadisk.downloader.close_target_file", apperr.KindIO, fmt.Errorf("close file: %w", err))
	}

	return &DownloadedFile{Path: targetPath, Size: written, IsHLSStream: false}, nil
}

// resumeDownload resumes a partial download using Range requests.
func (fd *FileDownloader) resumeDownload(
	ctx context.Context,
	file PublicDownload,
	out *os.File,
	currentOffset int64,
	total int64,
	progressWriter io.Writer,
) (int64, error) {
	const maxRangeParts = 256

	written := currentOffset
	for part := 0; written < total && part < maxRangeParts; part++ {
		rangeFile, err := fd.client.OpenDirectFileRange(ctx, file, written)
		if err != nil {
			return written, apperr.New("yadisk.downloader.open_range", apperr.KindNetwork, fmt.Errorf("open range at offset %d: %w", written, err))
		}

		// Handle server ignoring Range and returning full body
		if rangeFile.Offset == 0 && written > 0 {
			if err := out.Truncate(0); err != nil {
				_ = rangeFile.Body.Close()
				return written, apperr.New("yadisk.downloader.truncate_file", apperr.KindIO, fmt.Errorf("truncate file: %w", err))
			}
			if _, err := out.Seek(0, 0); err != nil {
				_ = rangeFile.Body.Close()
				return written, apperr.New("yadisk.downloader.seek_start", apperr.KindIO, fmt.Errorf("seek to start: %w", err))
			}
			written = 0
		}

		var chunkWriter io.Writer = out
		if progressWriter != nil {
			chunkWriter = io.MultiWriter(out, progressWriter)
		}
		chunkWritten, err := io.Copy(chunkWriter, rangeFile.Body)
		closeErr := rangeFile.Body.Close()
		if err != nil {
			return written, apperr.New("yadisk.downloader.copy_range", apperr.KindNetwork, fmt.Errorf("copy range: %w", err))
		}
		if closeErr != nil {
			return written, apperr.New("yadisk.downloader.close_range_body", apperr.KindIO, fmt.Errorf("close range body: %w", closeErr))
		}

		if chunkWritten <= 0 {
			return written, apperr.New("yadisk.downloader.no_progress", apperr.KindNetwork, fmt.Errorf("no progress made at offset %d", written))
		}

		written += chunkWritten
	}

	if written < total {
		return written, apperr.New("yadisk.downloader.incomplete_download", apperr.KindNetwork, fmt.Errorf("incomplete download: got %d of %d bytes", written, total))
	}

	return written, nil
}

// downloadViaHLS downloads a video using HLS streams (bypass for read_without_download).
func (fd *FileDownloader) downloadViaHLS(
	ctx context.Context,
	publicURL string,
	file PublicDownload,
	targetPath string,
	progressWriter io.Writer,
) (*DownloadedFile, error) {
	streams, err := fd.client.GetVideoStreams(ctx, publicURL, file.Path)
	if err != nil {
		return nil, apperr.Wrap("yadisk.downloader.get_video_streams", err)
	}

	if len(streams) == 0 {
		return nil, apperr.New("yadisk.downloader.video_streams_empty", apperr.KindIO, fmt.Errorf("no video streams available"))
	}

	best := ChooseBestVideoStream(streams)
	fd.log("downloading HLS stream", "file", file.Name, "quality", best.Dimension)

	out, err := os.Create(targetPath)
	if err != nil {
		return nil, apperr.New("yadisk.downloader.create_hls_file", apperr.KindIO, fmt.Errorf("create file: %w", err))
	}
	defer out.Close()

	if err := fd.client.DownloadHLSStream(ctx, best.URL, io.MultiWriter(out, progressWriter)); err != nil {
		os.Remove(targetPath)
		return nil, apperr.Wrap("yadisk.downloader.download_hls_stream", err)
	}

	// Get final file size
	stat, err := os.Stat(targetPath)
	if err != nil {
		return nil, apperr.New("yadisk.downloader.stat_hls_file", apperr.KindIO, err)
	}

	return &DownloadedFile{Path: targetPath, Size: stat.Size(), IsHLSStream: true}, nil
}

// log is a helper to call the log callback if set.
func (fd *FileDownloader) log(message string, fields ...interface{}) {
	if fd.onLog != nil {
		fd.onLog(message, fields...)
	}
}
