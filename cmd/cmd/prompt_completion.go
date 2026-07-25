package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/rivo/uniseg"
	"github.com/spf13/cobra"
)

const maxPromptVisibleCompletions = 6

type promptCandidate struct {
	Value       string
	Display     string
	Description string
}

type completionResult struct {
	Start       int
	End         int
	Quoted      bool
	QuoteClosed bool
	Candidates  []promptCandidate
	Err         error
}

type promptCompletionToken struct {
	value               string
	rawStart, rawEnd    int
	start, end          int
	quoted, quoteClosed bool
}

func (r *Root) completePrompt(ctx context.Context, line string, cursor int) completionResult {
	runes := []rune(line)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}

	tokens := splitPromptCompletionTokens(runes)
	active, ok := activePromptCompletionToken(tokens, cursor)
	if !ok {
		active = promptCompletionToken{rawStart: cursor, rawEnd: cursor, start: cursor, end: cursor}
	}

	args := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.rawEnd <= active.rawStart {
			args = append(args, token.value)
		}
	}

	root := r.newPromptRootCmd()
	command, _, err := root.Find(args)
	if err != nil || command == nil {
		return completionResult{Start: active.start, End: active.end, Quoted: active.quoted, QuoteClosed: active.quoteClosed}
	}
	kind, ok := command.Annotations["prompt_suggest"]
	if !ok {
		return completionResult{
			Start:       active.start,
			End:         active.end,
			Quoted:      active.quoted,
			QuoteClosed: active.quoteClosed,
			Candidates:  promptCommandCandidates(command, active.value),
		}
	}
	if r.client == nil || r.client.DialogCache == nil {
		return completionResult{Start: active.start, End: active.end, Quoted: active.quoted, QuoteClosed: active.quoteClosed, Err: fmt.Errorf("dialog cache is unavailable")}
	}

	peers, err := r.client.DialogCache.GetDialogPeers(ctx, promptPeerFilters(kind)...)
	if err != nil {
		return completionResult{Start: active.start, End: active.end, Quoted: active.quoted, QuoteClosed: active.quoteClosed, Err: fmt.Errorf("dialog cache: %w", err)}
	}

	return completionResult{
		Start:       active.start,
		End:         active.end,
		Quoted:      active.quoted,
		QuoteClosed: active.quoteClosed,
		Candidates:  promptCandidates(peers, active.value),
	}
}

func promptCommandCandidates(command *cobra.Command, query string) []promptCandidate {
	var candidates []promptCandidate
	for _, subcommand := range command.Commands() {
		if !subcommand.IsAvailableCommand() || !hasPrefixFold(subcommand.Name(), query) {
			continue
		}
		candidates = append(candidates, promptCandidate{
			Value:       subcommand.Name(),
			Display:     subcommand.Name(),
			Description: subcommand.Short,
		})
	}
	return candidates
}

func splitPromptCompletionTokens(line []rune) []promptCompletionToken {
	var tokens []promptCompletionToken
	var value strings.Builder
	rawStart, start, end := -1, -1, -1
	inQuotes := false
	quoted, quoteClosed := false, false
	flush := func(rawEnd int) {
		if rawStart < 0 {
			return
		}
		if end < start {
			end = start
		}
		tokens = append(tokens, promptCompletionToken{
			value:       value.String(),
			rawStart:    rawStart,
			rawEnd:      rawEnd,
			start:       start,
			end:         end,
			quoted:      quoted,
			quoteClosed: quoteClosed,
		})
		value.Reset()
		rawStart, start, end = -1, -1, -1
		quoted, quoteClosed = false, false
		inQuotes = false
	}

	for i := 0; i < len(line); i++ {
		r := line[i]
		if !inQuotes && (r == ' ' || r == '\t') {
			flush(i)
			continue
		}
		if r == '"' {
			if rawStart < 0 {
				rawStart = i
				start = i + 1
				end = start
				quoted = true
			}
			if inQuotes && i+1 < len(line) && line[i+1] == '"' {
				value.WriteRune('"')
				end = i + 2
				i++
				continue
			}
			inQuotes = !inQuotes
			if !inQuotes {
				end = i
				if quoted {
					quoteClosed = true
				}
			}
			continue
		}
		if rawStart < 0 {
			rawStart = i
			start = i
		}
		value.WriteRune(r)
		end = i + 1
	}
	flush(len(line))
	return tokens
}

func activePromptCompletionToken(tokens []promptCompletionToken, cursor int) (promptCompletionToken, bool) {
	for _, token := range tokens {
		if token.rawStart <= cursor && cursor <= token.rawEnd {
			return token, true
		}
	}
	return promptCompletionToken{}, false
}

func promptCandidates(peers []telegram.DialogPeer, query string) []promptCandidate {
	query = sanitizePromptPeerName(normalizePeerInput(query))
	nameCounts := make(map[string]int, len(peers))
	for _, peer := range peers {
		nameCounts[strings.ToLower(sanitizePromptPeerName(peer.Name()))]++
	}

	type rankedCandidate struct {
		promptCandidate
		rank int
	}
	candidates := make([]rankedCandidate, 0, len(peers))
	for _, peer := range peers {
		id := renderer.RenderTDLibPeerID(peer.TDLibPeerID())
		if isTDLibIDInput(query) {
			if !hasPrefixFold(id, query) {
				continue
			}
			candidates = append(candidates, rankedCandidate{
				promptCandidate: promptCandidate{
					Value:       id,
					Display:     sanitizePromptPeerName(peer.Name()),
					Description: fmt.Sprintf("%s | %s", dialogPeerType(peer), id),
				},
			})
			continue
		}

		candidate, ok := peerCandidate(peer, query)
		if !ok {
			continue
		}
		if nameCounts[strings.ToLower(candidate.Value)] > 1 {
			candidate.Value = id
		}
		candidate.Description = fmt.Sprintf("%s | %s", dialogPeerType(peer), id)
		candidates = append(candidates, rankedCandidate{
			promptCandidate: candidate,
			rank:            peerMatchRank(peer, id, query),
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return strings.ToLower(candidates[i].Display) < strings.ToLower(candidates[j].Display)
	})
	result := make([]promptCandidate, len(candidates))
	for i, candidate := range candidates {
		result[i] = candidate.promptCandidate
	}
	return result
}

func promptPeerFilters(kind string) []telegram.DialogPeerFilter {
	switch kind {
	case "user":
		return []telegram.DialogPeerFilter{telegram.OnlyUsersDialogPeerFilter()}
	case "chat":
		return []telegram.DialogPeerFilter{telegram.OnlyChatsDialogPeerFilter()}
	case "channel":
		return []telegram.DialogPeerFilter{telegram.OnlyChannelsDialogPeerFilter()}
	case "chatorchannel":
		return []telegram.DialogPeerFilter{telegram.OrDialogPeerFilter(
			telegram.OnlyChatsDialogPeerFilter(),
			telegram.OnlyChannelsDialogPeerFilter(),
		)}
	default:
		return nil
	}
}

func peerMatchRank(peer telegram.DialogPeer, id, query string) int {
	if isTDLibIDInput(query) {
		if hasPrefixFold(id, query) {
			return 0
		}
		return -1
	}
	if query == "" {
		return 2
	}

	for _, alias := range peer.SearchNames() {
		if strings.EqualFold(sanitizePromptPeerName(alias), query) {
			return 0
		}
	}
	for _, alias := range peer.SearchNames() {
		if hasPrefixFold(sanitizePromptPeerName(alias), query) {
			return 1
		}
	}
	for _, alias := range peer.SearchNames() {
		if containsFold(sanitizePromptPeerName(alias), query) {
			return 2
		}
	}
	return -1
}

func truncatePromptText(value string, width int) string {
	if width <= 0 || lipgloss.Width(value) <= width {
		return value
	}

	const suffix = "..."
	limit := width - lipgloss.Width(suffix)
	if limit <= 0 {
		return suffix[:width]
	}

	var b strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(value)
	for graphemes.Next() {
		cluster := graphemes.Str()
		clusterWidth := lipgloss.Width(cluster)
		if used+clusterWidth > limit {
			break
		}
		b.WriteString(cluster)
		used += clusterWidth
	}
	return b.String() + suffix
}

func formatPromptCandidate(candidate promptCandidate, width int) string {
	display := sanitizePromptModelText(candidate.Display)
	description := sanitizePromptModelText(candidate.Description)
	if peerType, peerID, ok := splitPromptPeerDescription(description); ok {
		return truncatePromptText(formatPromptPeerCandidate(display, peerType, peerID, width), width)
	}
	return truncatePromptText(formatPromptCommandCandidate(display, description, width), width)
}

func splitPromptPeerDescription(description string) (string, string, bool) {
	peerType, peerID, ok := strings.Cut(description, " | ")
	if !ok || peerType == "" || !strings.HasPrefix(peerID, "0x") {
		return "", "", false
	}
	return peerType, peerID, true
}

func formatPromptPeerCandidate(display, peerType, peerID string, width int) string {
	if width <= 0 {
		return ""
	}

	const fieldSeparator = "  "
	const typeSeparator = " | "
	const typeWidth = len("Channel")
	fullDescriptionWidth := typeWidth + lipgloss.Width(typeSeparator) + lipgloss.Width(peerID)
	if availableNameWidth := width - lipgloss.Width(fieldSeparator) - fullDescriptionWidth; availableNameWidth > 0 {
		return padPromptText(display, availableNameWidth) + fieldSeparator +
			padPromptText(peerType, typeWidth) + typeSeparator + peerID
	}

	if availableNameWidth := width - lipgloss.Width(fieldSeparator) - lipgloss.Width(peerID); availableNameWidth > 0 {
		return padPromptText(display, availableNameWidth) + fieldSeparator + peerID
	} else if availableNameWidth == 0 {
		return peerID
	}
	return truncatePromptText(peerID, width)
}

func padPromptText(value string, width int) string {
	value = truncatePromptText(value, width)
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func formatPromptCommandCandidate(display, description string, width int) string {
	if width <= 0 || description == "" {
		return truncatePromptText(display, width)
	}

	const fieldSeparator = "  "
	descriptionWidth := width - lipgloss.Width(display) - lipgloss.Width(fieldSeparator)
	if descriptionWidth < lipgloss.Width("...") {
		return truncatePromptText(display, width)
	}
	return display + fieldSeparator + truncatePromptText(description, descriptionWidth)
}
