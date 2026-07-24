package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	prompt "github.com/c-bata/go-prompt"
	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/downloader"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/mattn/go-runewidth"
	"github.com/spf13/cobra"
)

const maxPromptPeerSuggestionWidth = 48

type downloadOptions struct {
	limit      int
	user       int64
	offsetDate string
	single     bool
	hashtags   bool
	rewrite    bool
	dryRun     bool
	ps         bool
}

func (o *downloadOptions) newGetAllFilesOptions() ([]telegram.GetAllFilesOption, error) {
	var opts []telegram.GetAllFilesOption

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

func (o *downloadOptions) newGetFileOptions() ([]telegram.GetFileOption, error) {
	var opts []telegram.GetFileOption

	if o.single {
		opts = append(opts, telegram.GetFileWithGrouped(false))
	}

	return opts, nil
}

func (r *Root) downloadFilesFromPeer(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	getFileOptions, err := opts.newGetAllFilesOptions()
	if err != nil {
		return apperr.Wrap("cmd.download.history.options", err)
	}

	files, err := r.client.FileService.GetAllFiles(ctx, peer, getFileOptions...)
	if err != nil {
		return apperr.Wrap("cmd.download.history.get_all_files", err)
	}

	return apperr.Wrap("cmd.download.history.download", r.downloadFiles(ctx, files, opts))
}

func (r *Root) downloadFilesFromNewMessages(ctx context.Context, peer peers.Peer, opts downloadOptions) error {
	files, err := r.client.FileService.GetAllFilesFromNewMessages(ctx, peer)
	if err != nil {
		return apperr.Wrap("cmd.download.watcher.get_new_files", err)
	}

	return apperr.Wrap("cmd.download.watcher.download", r.downloadFiles(ctx, files, opts))
}

type trackerAdapter struct {
	renderer.Progress
}

var _ downloader.Tracker = (*trackerAdapter)(nil)

func (pa *trackerAdapter) WrapWriter(w io.Writer, msg string, size int64) downloader.TrackedWriter {
	return pa.BytesTracker(w, msg, size)
}

func newTrackerAdapter(p renderer.Progress) *trackerAdapter {
	return &trackerAdapter{p}
}

func (r *Root) downloadFiles(ctx context.Context, files <-chan telegram.File, opts downloadOptions) error {
	p := renderer.NewProgress()
	if opts.ps {
		p.EnablePS(ctx)
	}

	var downloaderOptions []downloader.Option
	downloaderOptions = append(downloaderOptions, downloader.WithRewrite(opts.rewrite))
	downloaderOptions = append(downloaderOptions, downloader.WithDryRun(opts.dryRun))
	downloaderOptions = append(downloaderOptions, downloader.WithTracker(newTrackerAdapter(p)))

	d, err := r.newDownloader(downloaderOptions...)
	if err != nil {
		return apperr.Wrap("cmd.download.new_downloader", err)
	}

	queue := make(chan downloader.File)
	go func() {
		defer close(queue)

		for {
			select {
			case <-ctx.Done():
				return

			case file, ok := <-files:
				if !ok {
					return
				}

				queue <- downloader.NewFile(file, downloader.WithSaveByHashtags(opts.hashtags))
			}
		}
	}()

	d.Start(ctx)
	d.AddDownloadQueue(ctx, queue)
	err = d.Stop(ctx)
	stats := d.Stats()
	renderer.RenderDownloadSummary(stats.Downloaded, stats.Skipped, stats.Failed)
	return apperr.Wrap("cmd.download.stop", err)
}

func sendSliceToChannel[T any](ctx context.Context, slice []*T) <-chan T {
	ch := make(chan T)
	go func() {
		defer close(ch)

		for _, elem := range slice {
			select {
			case ch <- *elem:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch
}

func (r *Root) downloadFilesFromMessage(ctx context.Context, peer peers.Peer, msgID int, opts downloadOptions) error {
	getFileOptions, err := opts.newGetFileOptions()
	if err != nil {
		return apperr.Wrap("cmd.download.message.options", err)
	}

	files, err := r.client.FileService.GetFilesFromMessage(ctx, peer, msgID, getFileOptions...)
	if err != nil {
		return apperr.Wrap("cmd.download.message.get_files", err)
	}

	return apperr.Wrap("cmd.download.message.download", r.downloadFiles(ctx, sendSliceToChannel(ctx, files), opts))
}

func parseTDLibPeerID(peerID string) (constant.TDLibPeerID, error) {
	parsed, err := strconv.ParseUint(strings.ToLower(strings.TrimPrefix(peerID, "0x")), 16, 64)
	if err != nil {
		return 0, err
	}

	return constant.TDLibPeerID(parsed), nil
}

func (r *Root) resolvePeer(ctx context.Context, arg string) (peers.Peer, error) {
	tdLibPeerID, err := parseTDLibPeerID(arg)
	if err == nil {
		peer, err := r.client.PeerService.ResolveTDLibID(ctx, tdLibPeerID)
		if err == nil {
			return peer, nil
		}
	}

	dialogPeers, err := r.client.DialogCache.GetDialogPeers(ctx)
	if err != nil {
		return nil, apperr.Wrap("cmd.resolve_peer.cache_lookup", err)
	}

	dialogPeer, err := resolveDialogPeerByInput(dialogPeers, arg)
	if err != nil {
		return nil, apperr.Wrap("cmd.resolve_peer.lookup", err)
	}

	peer, err := r.client.PeerService.ResolveTDLibID(ctx, dialogPeer.TDLibPeerID())
	if err != nil {
		return nil, apperr.Wrap("cmd.resolve_peer.lookup_tdlib_id", err)
	}

	return peer, nil
}

func peerInputArgs(cmd *cobra.Command, args []string) error {
	return cobra.MinimumNArgs(1)(cmd, args)
}

func peerInputArg(args []string) string {
	return strings.Join(args, " ")
}

func dialogPeerSuggest(peer telegram.DialogPeer, word string) (prompt.Suggest, bool) {
	id := renderer.RenderTDLibPeerID(peer.TDLibPeerID())
	if isTDLibIDInput(word) {
		if !strings.HasPrefix(strings.ToLower(id), strings.ToLower(word)) {
			return prompt.Suggest{}, false
		}

		return prompt.Suggest{
			Text: id,
		}, true
	}

	name := sanitizePromptPeerName(peer.Name())
	nameQuery := sanitizePromptPeerName(normalizePeerInput(word))
	if name == "" {
		return prompt.Suggest{}, false
	}
	if !dialogPeerAliasMatches(peer, nameQuery, containsFold) {
		return prompt.Suggest{}, false
	}

	return prompt.Suggest{
		Text: promptPeerSuggestionText(name),
	}, true
}

func resolveDialogPeerByInput(peers []telegram.DialogPeer, input string) (telegram.DialogPeer, error) {
	normalized := sanitizePromptPeerName(normalizePeerInput(input))
	var exact []telegram.DialogPeer
	for _, peer := range peers {
		if dialogPeerAliasMatches(peer, normalized, strings.EqualFold) {
			exact = append(exact, peer)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return telegram.DialogPeer{}, ambiguousPeerNameError(normalized, exact)
	}

	var prefix []telegram.DialogPeer
	for _, peer := range peers {
		if dialogPeerAliasMatches(peer, normalized, hasPrefixFold) {
			prefix = append(prefix, peer)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return telegram.DialogPeer{}, ambiguousPeerNameError(normalized, prefix)
	}

	return telegram.DialogPeer{}, fmt.Errorf("peer not found by name %q", normalized)
}

func dialogPeerAliasMatches(peer telegram.DialogPeer, query string, match func(string, string) bool) bool {
	for _, alias := range peer.SearchNames() {
		if match(sanitizePromptPeerName(alias), query) {
			return true
		}
	}
	return false
}

func ambiguousPeerNameError(input string, peers []telegram.DialogPeer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous peer name %q, candidates:", input)
	for _, peer := range peers {
		fmt.Fprintf(
			&b,
			"\n- %s | %s | %s",
			peer.Name(),
			renderer.RenderTDLibPeerID(peer.TDLibPeerID()),
			dialogPeerType(peer),
		)
	}

	return fmt.Errorf("%s", b.String())
}

func promptPeerSuggestionText(name string) string {
	return runewidth.Truncate(sanitizePromptPeerName(name), maxPromptPeerSuggestionWidth, "")
}

func sanitizePromptPeerName(name string) string {
	var b strings.Builder
	lastWasSpace := true
	for _, r := range name {
		switch {
		case unicode.IsSpace(r) || unicode.IsControl(r):
			if !lastWasSpace {
				b.WriteByte(' ')
				lastWasSpace = true
			}
		case unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsPunct(r):
			b.WriteRune(r)
			lastWasSpace = false
		}
	}

	return strings.TrimSpace(b.String())
}

func dialogPeerType(peer telegram.DialogPeer) string {
	switch {
	case peer.TDLibPeerID().IsUser():
		return "User"
	case peer.TDLibPeerID().IsChat():
		return "Chat"
	case peer.TDLibPeerID().IsChannel():
		return "Channel"
	default:
		return "Unknown"
	}
}

func normalizePeerInput(input string) string {
	input = strings.TrimSpace(input)
	if len(input) >= 2 && input[0] == '"' && input[len(input)-1] == '"' {
		input = strings.Trim(input, `"`)
		input = strings.ReplaceAll(input, `""`, `"`)
	} else if strings.HasPrefix(input, `"`) {
		input = strings.TrimPrefix(input, `"`)
	}
	return input
}

func isTDLibIDInput(input string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(input)), "0x")
}

func containsFold(s string, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(strings.TrimSpace(substr)))
}

func hasPrefixFold(s string, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(s), strings.ToLower(strings.TrimSpace(prefix)))
}
