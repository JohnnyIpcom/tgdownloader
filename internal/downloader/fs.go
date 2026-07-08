package downloader

import (
	"fmt"
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
	fsOnce sync.Once
	fs     afero.Fs
	fsErr  error
)

func GetFS(cfg config.Config, log *log.Logger) (afero.Fs, error) {
	fsOnce.Do(func() {
		switch strings.ToLower(cfg.GetString("type")) {
		case "local":
			fs = afero.NewOsFs()

		case "dropbox":
			port := cfg.GetInt("dropbox.port")
			client := <-oauth2server.RunOAuth2Server(port, oauth2.Config{
				ClientID:     cfg.GetString("dropbox.oauth2.id"),
				ClientSecret: cfg.GetString("dropbox.oauth2.secret"),
				RedirectURL:  fmt.Sprintf("http://localhost:%d/oauth2/callback", port),
				Endpoint: oauth2.Endpoint{
					AuthURL:  "https://www.dropbox.com/oauth2/authorize",
					TokenURL: "https://api.dropboxapi.com/oauth2/token",
				},
			})
			if client == nil {
				fsErr = apperr.New("downloader.get_fs.oauth2", apperr.KindAuth, fmt.Errorf("oauth2 authorization failed: no client returned"))
				return
			}

			dfs, err := dropbox.NewFs(client, log)
			if err != nil {
				fsErr = apperr.New("downloader.get_fs.dropbox", apperr.KindConfig, fmt.Errorf("create dropbox filesystem: %w", err))
				return
			}

			fs = dfs

		default:
			fsErr = apperr.New("downloader.get_fs.type", apperr.KindConfig, fmt.Errorf("invalid downloader type %q", cfg.GetString("type")))
			return
		}
	})

	if fsErr != nil {
		return nil, fsErr
	}

	if fs == nil {
		return nil, apperr.New("downloader.get_fs.init", apperr.KindInternal, fmt.Errorf("downloader filesystem is not initialized"))
	}

	return fs, nil
}
