package dropbox

import (
	"context"
	"fmt"
	"net/http"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

func RunOauth2Server(ctx context.Context, cfg config.Config, log *zap.Logger) <-chan *http.Client {
	client := make(chan *http.Client, 1)

	go func() {
		defer close(client)

		port := cfg.GetInt("port")

		fmt.Printf("Go to http://localhost:%d to authorize the dropbox client\n", port)
		conf := oauth2.Config{
			ClientID:     cfg.GetString("oauth2.id"),
			ClientSecret: cfg.GetString("oauth2.secret"),
			RedirectURL:  fmt.Sprintf("http://localhost:%d/oauth2/callback", port),
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://www.dropbox.com/oauth2/authorize",
				TokenURL: "https://api.dropboxapi.com/oauth2/token",
			},
		}

		gin.SetMode(gin.ReleaseMode)

		r := gin.New()
		r.Use(ginzap.Ginzap(log.Named("gin"), time.RFC3339, true))

		r.GET("/", func(c *gin.Context) {
			url := conf.AuthCodeURL("state", oauth2.AccessTypeOffline)
			c.Redirect(http.StatusFound, url)
		})

		r.GET("/oauth2/callback", func(c *gin.Context) {
			token, err := conf.Exchange(c, c.Query("code"))
			if err != nil {
				c.String(http.StatusInternalServerError, err.Error())
				return
			}

			client <- conf.Client(c, token)
			c.String(http.StatusOK, "Success")

			fmt.Println("Dropbox client authorized")
		})

		srv := &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: r,
		}

		go srv.ListenAndServe()

		<-ctx.Done()
		srv.Shutdown(ctx)
	}()

	return client
}
