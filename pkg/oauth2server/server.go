package oauth2server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

func RunOAuth2Server(ctx context.Context, writer io.Writer, port int, cfg oauth2.Config) (*http.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if writer == nil {
		writer = io.Discard
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, apperr.New("oauth2server.listen", apperr.KindNetwork, err)
	}

	type oauthResult struct {
		client *http.Client
		err    error
	}
	result := make(chan oauthResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:     "oauthstate",
			Value:    url.QueryEscape(uuid.New().String()),
			Expires:  time.Now().Add(10 * time.Minute),
			HttpOnly: true,
		}
		http.SetCookie(w, cookie)
		authorizationURL := cfg.AuthCodeURL(cookie.Value, oauth2.AccessTypeOffline)
		http.Redirect(w, r, authorizationURL, http.StatusFound)
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

		token, err := cfg.Exchange(r.Context(), values.Get("code"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		select {
		case result <- oauthResult{client: cfg.Client(ctx, token)}:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Success"))
		case <-ctx.Done():
			http.Error(w, "Authorization canceled", http.StatusRequestTimeout)
		}
	})

	srv := &http.Server{
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	serveErr := make(chan error, 1)
	go func() {
		err := srv.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	_, _ = fmt.Fprintf(writer, "Go to http://localhost:%d to authorize client\n", actualPort)

	var authorized *http.Client
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case res := <-result:
		authorized, err = res.client, res.err
		if err == nil {
			_, _ = fmt.Fprintln(writer, "Client authorized")
		}
	case serveFailure := <-serveErr:
		if serveFailure != nil {
			err = apperr.New("oauth2server.serve", apperr.KindNetwork, serveFailure)
		} else {
			err = errors.New("oauth2 server stopped before authorization")
		}
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelShutdown()
	_ = srv.Shutdown(shutdownCtx)
	_ = listener.Close()
	return authorized, err
}
