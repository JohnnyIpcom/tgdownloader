package cmd

import (
	"context"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/downloader"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type downloadOptions struct {
	limit         int
	user          int64
	offsetDate    string
	hashtags      bool
	saveOnlyIfNew bool
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

func (o *downloadOptions) newSaveFileOptions() []downloader.SaveFileOption {
	var opts []downloader.SaveFileOption

	if o.hashtags {
		opts = append(opts, downloader.SaveByHashtags())
	}

	if o.saveOnlyIfNew {
		opts = append(opts, downloader.SaveOnlyIfNew())
	}

	return opts
}

func (r *Root) downloadFiles(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	getFileOptions, err := opts.newGetFileOptions()
	if err != nil {
		return err
	}

	files, err := r.client.FileService.GetFiles(ctx, peer, getFileOptions...)
	if err != nil {
		return err
	}

	d, err := r.newDownloader()
	if err != nil {
		return err
	}

	d.Start(ctx)
	d.AddDownloadQueue(ctx, files, opts.newSaveFileOptions()...)
	return d.Stop()
}

func (r *Root) downloadFilesFromNewMessages(ctx context.Context, ID int64, opts downloadOptions) error {
	files, err := r.client.FileService.GetFilesFromNewMessages(ctx, ID)
	if err != nil {
		return err
	}

	d, err := r.newDownloader()
	if err != nil {
		return err
	}

	d.Start(ctx)
	d.AddDownloadQueue(ctx, files, opts.newSaveFileOptions()...)
	return d.Stop()
}
