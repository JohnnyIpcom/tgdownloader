package cmd

import (
	"errors"
	"testing"
)

func TestYandexDownloadSummaryAddLinkResult(t *testing.T) {
	t.Parallel()

	s := yandexDownloadSummary{}
	s.AddLinkResult(3, 2, nil)
	s.AddLinkResult(1, 0, errors.New("boom"))

	downloaded, skipped, failed := s.Values()
	if downloaded != 4 || skipped != 2 || failed != 1 {
		t.Fatalf("unexpected summary values: downloaded=%d skipped=%d failed=%d", downloaded, skipped, failed)
	}
}

func TestYandexDownloadSummaryMarkFailed(t *testing.T) {
	t.Parallel()

	s := yandexDownloadSummary{}
	s.MarkFailed()
	s.MarkFailed()

	downloaded, skipped, failed := s.Values()
	if downloaded != 0 || skipped != 0 || failed != 2 {
		t.Fatalf("unexpected summary values: downloaded=%d skipped=%d failed=%d", downloaded, skipped, failed)
	}
}
