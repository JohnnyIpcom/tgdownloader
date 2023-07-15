package cmd

import (
	"context"
	"time"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type downloadOptions struct {
	limit      int
	user       int64
	offsetDate string
	hashtags   bool
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

func (r *Root) downloadFiles(ctx context.Context, peer telegram.PeerInfo, saveByHashtags bool, opts ...telegram.GetFileOption) error {
	files, err := r.client.FileService.GetFiles(ctx, peer, opts...)
	if err != nil {
		return err
	}

	d, err := r.newDownloader()
	if err != nil {
		return err
	}

	d.Start(ctx)
	d.AddDownloadQueue(ctx, files, saveByHashtags)
	return d.Stop()
}

func (r *Root) downloadFilesFromNewMessages(ctx context.Context, ID int64, saveByHashtags bool) error {
	files, err := r.client.FileService.GetFilesFromNewMessages(ctx, ID)
	if err != nil {
		return err
	}

	d, err := r.newDownloader()
	if err != nil {
		return err
	}

	d.Start(ctx)
	d.AddDownloadQueue(ctx, files, true)
	return d.Stop()
}
