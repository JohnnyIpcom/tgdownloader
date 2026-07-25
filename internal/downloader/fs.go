package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/dropbox"
	"github.com/johnnyipcom/tgdownloader/pkg/oauth2server"
	"github.com/spf13/afero"
	"golang.org/x/oauth2"
)

var (
	fsMu sync.Mutex
	fs   afero.Fs
)

func GetFS(ctx context.Context, cfg config.Config, log *log.Logger, writer io.Writer) (afero.Fs, error) {
	fsMu.Lock()
	defer fsMu.Unlock()
	if fs != nil {
		return fs, nil
	}

	switch strings.ToLower(cfg.GetString("type")) {
	case "local":
		fs = afero.NewOsFs()

	case "dropbox":
		port := cfg.GetInt("dropbox.port")
		client, err := oauth2server.RunOAuth2Server(ctx, writer, port, oauth2.Config{
			ClientID:     cfg.GetString("dropbox.oauth2.id"),
			ClientSecret: cfg.GetString("dropbox.oauth2.secret"),
			RedirectURL:  fmt.Sprintf("http://localhost:%d/oauth2/callback", port),
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://www.dropbox.com/oauth2/authorize",
				TokenURL: "https://api.dropboxapi.com/oauth2/token",
			},
		})
		if err != nil {
			kind := apperr.KindAuth
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				kind = apperr.KindCancel
			}
			return nil, apperr.New("downloader.get_fs.oauth2", kind, err)
		}
		if client == nil {
			return nil, apperr.New("downloader.get_fs.oauth2", apperr.KindAuth, fmt.Errorf("oauth2 authorization failed: no client returned"))
		}

		dfs, err := dropbox.NewFs(client, log)
		if err != nil {
			return nil, apperr.New("downloader.get_fs.dropbox", apperr.KindConfig, fmt.Errorf("create dropbox filesystem: %w", err))
		}
		fs = dfs

	default:
		return nil, apperr.New("downloader.get_fs.type", apperr.KindConfig, fmt.Errorf("invalid downloader type %q", cfg.GetString("type")))
	}

	if fs == nil {
		return nil, apperr.New("downloader.get_fs.init", apperr.KindInternal, fmt.Errorf("downloader filesystem is not initialized"))
	}

	return fs, nil
}
