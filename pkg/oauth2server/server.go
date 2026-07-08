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
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"golang.org/x/oauth2"
)

var errInvalidOAuthState = errors.New("invalid oauth2 state")

func validateOAuthCallbackState(rawQuery string, cookie *http.Cookie) (url.Values, error) {
	if cookie == nil {
		return nil, apperr.New("oauth2server.validate_callback.cookie", apperr.KindAuth, errInvalidOAuthState)
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, apperr.New("oauth2server.validate_callback.query", apperr.KindConfig, err)
	}

	if values.Get("state") != cookie.Value {
		return nil, apperr.New("oauth2server.validate_callback.state", apperr.KindAuth, errInvalidOAuthState)
	}

	return values, nil
}

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
			if err != nil {
				if errors.Is(err, http.ErrNoCookie) {
					http.Error(w, "Invalid OAuth2 state", http.StatusBadRequest)
					return
				}

				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			values, err := validateOAuthCallbackState(r.URL.RawQuery, cookie)
			if err != nil {
				if errors.Is(err, errInvalidOAuthState) {
					http.Error(w, "Invalid OAuth2 state", http.StatusBadRequest)
					return
				}

				http.Error(w, err.Error(), http.StatusInternalServerError)
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

		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "oauth2 server failed: %v\n", err)
				stop()
			}
		}()

		<-ctx.Done()
		srv.Shutdown(ctx)
	}()

	return client
}
