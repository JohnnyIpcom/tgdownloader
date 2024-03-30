package cmd

import (
	"context"
	"io"
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
	single     bool
	hashtags   bool
	rewrite    bool
	dryRun     bool
}

func (o *downloadOptions) newGetAllFilesOptions() ([]telegram.GetAllFilesOption, error) {
	var opts []telegram.GetAllFilesOption

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

func (o *downloadOptions) newGetFileOptions() ([]telegram.GetFileOption, error) {
	var opts []telegram.GetFileOption

	if o.single {
		opts = append(opts, telegram.GetFileWithGrouped(false))
	}

	return opts, nil
}

func (r *Root) downloadFilesFromPeer(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	getFileOptions, err := opts.newGetAllFilesOptions()
	if err != nil {
		return err
	}

	files, err := r.client.FileService.GetAllFiles(ctx, peer, getFileOptions...)
	if err != nil {
		return err
	}

	return r.downloadFiles(ctx, files, opts)
}

func (r *Root) downloadFilesFromNewMessages(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	files, err := r.client.FileService.GetAllFilesFromNewMessages(ctx, peer)
	if err != nil {
		return err
	}

	return r.downloadFiles(ctx, files, opts)
}

type trackerAdapter struct {
	renderer.Progress
}

var _ downloader.Tracker = (*trackerAdapter)(nil)

func (pa *trackerAdapter) WrapWriter(w io.Writer, msg string, size int64) downloader.TrackedWriter {
	return pa.BytesTracker(w, msg, size)
}

func newTrackerAdapter(p renderer.Progress) *trackerAdapter {
	return &trackerAdapter{p}
}

func (r *Root) downloadFiles(ctx context.Context, files <-chan telegram.File, opts downloadOptions) error {
	p := renderer.NewProgress()
	p.EnablePS(ctx)

	var downloaderOptions []downloader.Option
	downloaderOptions = append(downloaderOptions, downloader.WithRewrite(opts.rewrite))
	downloaderOptions = append(downloaderOptions, downloader.WithDryRun(opts.dryRun))
	downloaderOptions = append(downloaderOptions, downloader.WithTracker(newTrackerAdapter(p)))

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

				queue <- downloader.NewFile(file, downloader.WithSaveByHashtags(opts.hashtags))
			}
		}
	}()

	d.Start(ctx)
	d.AddDownloadQueue(ctx, queue)
	return d.Stop(ctx)
}

func makeChannelFromSlice(ctx context.Context, files []*telegram.File) <-chan telegram.File {
	ch := make(chan telegram.File)
	go func() {
		defer close(ch)

		for _, file := range files {
			select {
			case ch <- *file:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

func (r *Root) downloadFilesFromMessage(ctx context.Context, peer peers.Peer, msgID int, opts downloadOptions) error {
	getFileOptions, err := opts.newGetFileOptions()
	if err != nil {
		return err
	}

	files, err := r.client.FileService.GetFilesFromMessage(ctx, peer, msgID, getFileOptions...)
	if err != nil {
		return err
	}

	return r.downloadFiles(ctx, makeChannelFromSlice(ctx, files), opts)
}

func parseTDLibPeerID(peerID string) (constant.TDLibPeerID, error) {
	parsed, err := strconv.ParseUint(strings.ToLower(strings.TrimPrefix(peerID, "0x")), 16, 64)
	if err != nil {
		return 0, err
	}

	return constant.TDLibPeerID(parsed), nil
}

func (r *Root) resolvePeer(ctx context.Context, arg string) (peers.Peer, error) {
	tdLibPeerID, err := parseTDLibPeerID(arg)
	if err != nil {
		return nil, err
	}

	return r.client.PeerService.ResolveTDLibID(ctx, tdLibPeerID)
}
