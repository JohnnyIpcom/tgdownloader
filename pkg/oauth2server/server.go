package oauth2server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

func RunOAuth2Server(port int, cfg oauth2.Config) <-chan *http.Client {
	client := make(chan *http.Client, 1)

	go func() {
		defer close(client)

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()

		fmt.Printf("Go to http://localhost:%d to authorize client\n", port)

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			cookie := &http.Cookie{
				Name:     "oauthstate",
				Value:    url.QueryEscape(uuid.New().String()),
				Expires:  time.Now().Add(10 * time.Minute),
				HttpOnly: true,
			}

			http.SetCookie(w, cookie)

			url := cfg.AuthCodeURL(cookie.Value, oauth2.AccessTypeOffline)
			http.Redirect(w, r, url, http.StatusFound)
		})

		mux.HandleFunc("/oauth2/callback", func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("oauthstate")
			if err != nil && !errors.Is(err, http.ErrNoCookie) {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			values, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			if values.Get("state") != cookie.Value {
				http.Error(w, "Invalid OAuth2 state", http.StatusBadRequest)
				return
			}

			token, err := cfg.Exchange(context.Background(), values.Get("code"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			client <- cfg.Client(ctx, token)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Success"))

			fmt.Println("Client authorized")
			stop()
		})

		srv := &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		}

		go srv.ListenAndServe()

		<-ctx.Done()
		srv.Shutdown(ctx)
	}()

	return client
}
