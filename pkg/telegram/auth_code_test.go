package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/td/tg"
	configviper "github.com/johnnyipcom/tgdownloader/pkg/config/viper"
	"go.uber.org/zap"
)

type recordingCodeProvider struct {
	ctx      context.Context
	sentCode *tg.AuthSentCode
	code     string
	err      error
}

func (p *recordingCodeProvider) Code(ctx context.Context, sentCode *tg.AuthSentCode) (string, error) {
	p.ctx = ctx
	p.sentCode = sentCode
	return p.code, p.err
}

func TestWithCodeProviderUsesInjectedProvider(t *testing.T) {
	provider := &recordingCodeProvider{code: "12345"}
	client := newCodeProviderTestClient(t, WithCodeProvider(provider))
	ctx := context.WithValue(context.Background(), struct{}{}, "value")
	sentCode := &tg.AuthSentCode{}

	code, err := client.codeProvider.Code(ctx, sentCode)
	if err != nil {
		t.Fatalf("Code() error = %v", err)
	}
	if code != "12345" || provider.ctx != ctx || provider.sentCode != sentCode {
		t.Fatalf("provider call = code %q ctx %v sentCode %p", code, provider.ctx, provider.sentCode)
	}
}

func TestDefaultCodeProviderReturnsExplicitError(t *testing.T) {
	client := newCodeProviderTestClient(t)
	_, err := client.codeProvider.Code(context.Background(), &tg.AuthSentCode{})
	if !errors.Is(err, ErrCodeProviderUnavailable) {
		t.Fatalf("Code() error = %v, want ErrCodeProviderUnavailable", err)
	}
}

func newCodeProviderTestClient(t *testing.T, options ...ClientOption) *Client {
	t.Helper()
	cfg := configviper.NewConfig()
	cfg.Set("app.id", 1)
	cfg.Set("app.hash", "hash")
	cfg.Set("storage.path", t.TempDir()+"/storage.db")
	cfg.Set("rate.limit", time.Millisecond)
	cfg.Set("rate.burst", 1)
	cfg.Set("updates.disable", true)

	client, err := NewClient(cfg, zap.NewNop(), options...)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	})
	return client
}
