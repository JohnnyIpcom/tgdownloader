package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/johnnyipcom/tgdownloader/internal/downloader"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
)

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

func (r *Root) downloadFilesFromPeer(ctx context.Context, writer io.Writer, peer peers.Peer, opts downloadOptions) error {
	if renderer.HasEventSink(ctx) {
		renderer.RenderDownloadPlan(writer, renderer.DownloadPlan{
			Name:      peer.VisibleName(),
			Type:      promptResolvedPeerType(peer),
			PeerID:    renderer.RenderTDLibPeerID(peer.TDLibPeerID()),
			OutputDir: r.cfg.GetString("downloader.dir.output"),
			Rewrite:   opts.rewrite,
			DryRun:    opts.dryRun,
		})
	}

	getFileOptions, err := opts.newGetAllFilesOptions()
	if err != nil {
		return apperr.Wrap("cmd.download.history.options", err)
	}

	files, err := r.client.FileService.GetAllFiles(ctx, peer, getFileOptions...)
	if err != nil {
		return apperr.Wrap("cmd.download.history.get_all_files", err)
	}

	return apperr.Wrap("cmd.download.history.download", r.downloadFiles(ctx, writer, files, opts))
}

func (r *Root) downloadFilesFromNewMessages(ctx context.Context, writer io.Writer, peer peers.Peer, opts downloadOptions) error {
	files, err := r.client.FileService.GetAllFilesFromNewMessages(ctx, peer)
	if err != nil {
		return apperr.Wrap("cmd.download.watcher.get_new_files", err)
	}

	return apperr.Wrap("cmd.download.watcher.download", r.downloadFiles(ctx, writer, files, opts))
}

type trackerAdapter struct {
	renderer.Progress
}

type downloadScanProgress struct {
	tracker renderer.Tracker
	found   int64
}

func newDownloadScanProgress(tracker renderer.Tracker) *downloadScanProgress {
	return &downloadScanProgress{tracker: tracker}
}

func (p *downloadScanProgress) FileFound(stats downloader.Stats) {
	p.found++
	p.tracker.Increment(1)
	p.updateMessage("Scanning history", stats)
}

func (p *downloadScanProgress) ScanningDone(stats downloader.Stats) {
	p.updateMessage("Scanning history complete", stats)
}

func (p *downloadScanProgress) Finish(stats downloader.Stats) {
	p.ScanningDone(stats)
	p.tracker.Done()
}

func (p *downloadScanProgress) updateMessage(prefix string, stats downloader.Stats) {
	p.tracker.UpdateMessage(fmt.Sprintf(
		"%s: found=%d skipped=%d failed=%d",
		prefix,
		p.found,
		stats.Skipped,
		stats.Failed,
	))
}

var _ downloader.Tracker = (*trackerAdapter)(nil)

func (pa *trackerAdapter) WrapWriter(w io.Writer, msg string, size int64) downloader.TrackedWriter {
	return pa.BytesTracker(w, msg, size)
}

func newTrackerAdapter(p renderer.Progress) *trackerAdapter {
	return &trackerAdapter{p}
}

func (r *Root) downloadFiles(ctx context.Context, writer io.Writer, files <-chan telegram.File, opts downloadOptions) error {
	startedAt := time.Now()
	p := renderer.NewProgressForContext(ctx)
	if opts.ps {
		p.EnablePS(ctx)
	}

	var downloaderOptions []downloader.Option
	var scanProgress *downloadScanProgress
	downloaderOptions = append(downloaderOptions, downloader.WithRewrite(opts.rewrite))
	downloaderOptions = append(downloaderOptions, downloader.WithDryRun(opts.dryRun))
	downloaderOptions = append(downloaderOptions, downloader.WithTracker(newTrackerAdapter(p)))
	downloaderOptions = append(downloaderOptions, downloader.WithOnComplete(func(stats downloader.Stats) {
		if scanProgress != nil {
			scanProgress.Finish(stats)
		}
	}))

	d, err := r.newDownloader(ctx, writer, downloaderOptions...)
	if err != nil {
		return apperr.Wrap("cmd.download.new_downloader", err)
	}
	scanProgress = newDownloadScanProgress(p.UnitsTracker("Scanning history", 0))
	queue := make(chan downloader.File)
	go func() {
		defer func() {
			close(queue)
			scanProgress.ScanningDone(d.Stats())
		}()

		for {
			select {
			case <-ctx.Done():
				return

			case file, ok := <-files:
				if !ok {
					return
				}

				scanProgress.FileFound(d.Stats())
				select {
				case queue <- downloader.NewFile(file, downloader.WithSaveByHashtags(opts.hashtags)):
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	d.Start(ctx)
	d.AddDownloadQueue(ctx, queue)
	err = d.Stop(ctx)
	stats := d.Stats()
	if renderer.HasEventSink(ctx) {
		renderer.RenderDownloadSummaryDetails(
			writer,
			stats.Downloaded,
			stats.Skipped,
			stats.Failed,
			time.Since(startedAt),
			r.cfg.GetString("downloader.dir.output"),
		)
	} else {
		renderer.RenderDownloadSummary(writer, stats.Downloaded, stats.Skipped, stats.Failed)
	}
	return apperr.Wrap("cmd.download.stop", err)
}

func promptResolvedPeerType(peer peers.Peer) string {
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

func (r *Root) downloadFilesFromMessage(ctx context.Context, writer io.Writer, peer peers.Peer, msgID int, opts downloadOptions) error {
	getFileOptions, err := opts.newGetFileOptions()
	if err != nil {
		return apperr.Wrap("cmd.download.message.options", err)
	}

	files, err := r.client.FileService.GetFilesFromMessage(ctx, peer, msgID, getFileOptions...)
	if err != nil {
		return apperr.Wrap("cmd.download.message.get_files", err)
	}

	return apperr.Wrap("cmd.download.message.download", r.downloadFiles(ctx, writer, sendSliceToChannel(ctx, files), opts))
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

func peerCandidate(peer telegram.DialogPeer, query string) (promptCandidate, bool) {
	name := sanitizePromptPeerName(peer.Name())
	if name == "" || !dialogPeerAliasMatches(peer, query, containsFold) {
		return promptCandidate{}, false
	}
	return promptCandidate{Value: name, Display: name}, true
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

func sanitizePromptPeerName(name string) string {
	return sanitizePromptModelText(name)
}

func sanitizePromptModelText(value string) string {
	return sanitizePromptText(value, false)
}

func sanitizePromptModelLine(value string) string {
	return sanitizePromptText(value, true)
}

func sanitizePromptText(value string, preserveSpaces bool) string {
	return renderer.SanitizeTerminalText(value, preserveSpaces)
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
