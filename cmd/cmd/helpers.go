package cmd

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/downloader"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type downloadOptions struct {
	limit      int
	user       int64
	offsetDate string
	hashtags   bool
	rewrite    bool
	dryRun     bool
}

func (o *downloadOptions) newGetFileOptions() ([]telegram.GetFileOption, error) {
	var opts []telegram.GetFileOption

	if o.user > 0 {
		opts = append(opts, telegram.GetFileWithUserID(o.user))
	}

	if o.limit > 0 {
		opts = append(opts, telegram.GetFileWithLimit(o.limit))
	}

	if o.offsetDate != "" {
		offsetDate, err := time.Parse("2006-01-02 15:04:05", o.offsetDate)
		if err != nil {
			return nil, err
		}

		opts = append(opts, telegram.GetFileWithOffsetDate(int(offsetDate.Unix())))
	}

	return opts, nil
}

func (r *Root) downloadFilesFromPeer(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	getFileOptions, err := opts.newGetFileOptions()
	if err != nil {
		return err
	}

	files, err := r.client.FileService.GetFiles(ctx, peer, getFileOptions...)
	if err != nil {
		return err
	}

	return r.downloadFiles(ctx, files, opts)
}

func (r *Root) downloadFilesFromNewMessages(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	files, err := r.client.FileService.GetFilesFromNewMessages(ctx, peer)
	if err != nil {
		return err
	}

	return r.downloadFiles(ctx, files, opts)
}

func (r *Root) downloadFiles(ctx context.Context, files <-chan telegram.File, opts downloadOptions) error {
	tracker := renderer.NewDownloadTracker()
	defer tracker.Stop()

	var downloaderOptions []downloader.Option
	downloaderOptions = append(downloaderOptions, downloader.WithRewrite(opts.rewrite))
	downloaderOptions = append(downloaderOptions, downloader.WithDryRun(opts.dryRun))
	downloaderOptions = append(downloaderOptions, downloader.WithTracker(tracker))

	d, err := r.newDownloader(downloaderOptions...)
	if err != nil {
		return err
	}

	queue := make(chan downloader.File)
	go func() {
		defer close(queue)

		for {
			select {
			case <-ctx.Done():
				return

			case file, ok := <-files:
				if !ok {
					return
				}

				subdirs := make([]string, 0, 2)
				metadata := file.Metadata()
				if metadata != nil {
					if peername, ok := metadata["peername"]; ok {
						subdirs = append(subdirs, peername.(string))
					}

					if opts.hashtags {
						if hashtags, ok := metadata["hashtags"]; ok {
							subdirs = append(subdirs, hashtags.([]string)...)
						}
					}
				}

				var fileOptions []downloader.FileOption
				if len(subdirs) > 0 {
					fileOptions = append(fileOptions, downloader.WithSubdirs(subdirs...))
				}

				queue <- downloader.NewFile(file, fileOptions...)
			}
		}
	}()

	d.Start(ctx)
	d.AddDownloadQueue(ctx, queue)
	return d.Stop()
}

func (r *Root) parseTDLibPeerID(peerID string) (constant.TDLibPeerID, error) {
	parsed, err := strconv.ParseUint(strings.ToLower(strings.TrimPrefix(peerID, "0x")), 16, 64)
	if err != nil {
		return 0, err
	}

	return constant.TDLibPeerID(parsed), nil
}
