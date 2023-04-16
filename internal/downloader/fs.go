package downloader

import (
	"fmt"
	"strings"
	"sync"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/dropbox"
	"github.com/johnnyipcom/tgdownloader/pkg/oauth2server"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

var (
	fsOnce sync.Once
	fs     afero.Fs
)

func getDownloaderFS(cfg config.Config, log *zap.Logger) afero.Fs {
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

			dfs, err := dropbox.NewFs(client, zap.NewStdLog(log.Named("dropbox")))
			if err != nil {
				panic(err)
			}

			fs = dfs

		default:
			panic("invalid downloader type")
		}
	})

	return fs
}
