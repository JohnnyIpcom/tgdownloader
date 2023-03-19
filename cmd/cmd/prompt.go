package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	prompt "github.com/c-bata/go-prompt"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func (r *Root) getPeerSuggestions(ctx context.Context, word string, peerType string) []prompt.Suggest {
	var filter telegram.PeerCacheInfoFilter
	switch peerType {
	case "user":
		filter = telegram.OnlyUsersPeerCacheInfoFilter()
	case "chat":
		filter = telegram.OnlyChatsPeerCacheInfoFilter()
	case "channel":
		filter = telegram.OnlyChannelsPeerCacheInfoFilter()
	}

	peers, err := r.client.CacheService.CollectPeersFromCache(ctx, filter)
	if err != nil {
		return nil
	}

	suggestions := make([]prompt.Suggest, 0, len(peers))
	for _, peer := range peers {
		suggestions = append(suggestions, prompt.Suggest{
			Text:        strconv.FormatInt(peer.ID, 10),
			Description: renderer.ReplaceAllEmojis(peer.Peer.Name),
		})
	}

	return prompt.FilterHasPrefix(suggestions, word, true)
}

func (r *Root) getVerbositySuggestions(word string) []prompt.Suggest {
	levels := []prompt.Suggest{
		{Text: "debug"},
		{Text: "info"},
		{Text: "warn"},
		{Text: "error"},
		{Text: "fatal"},
		{Text: "panic"},
	}

	return prompt.FilterHasPrefix(levels, word, true)
}

func (r *Root) getTypeSuggestions(word string) []prompt.Suggest {
	types := []prompt.Suggest{
		{Text: "user"},
		{Text: "chat"},
		{Text: "channel"},
	}

	return prompt.FilterHasPrefix(types, word, true)
}

func (r *Root) promptCompleter(d prompt.Document) []prompt.Suggest {
	args := strings.Fields(d.CurrentLine())
	word := d.GetWordBeforeCursor()

	cmd := r.cmdRoot
	if found, _, err := cmd.Find(args); err == nil {
		cmd = found
	}

	lastArg := ""
	if len(args) > 0 {
		lastArg = args[len(args)-1]
	}

	switch lastArg {
	case "--verbosity":
		return r.getVerbositySuggestions(word)

	case "--type":
		return r.getTypeSuggestions(word)
	default:
	}

	if strings.HasPrefix(word, "-") {
		var flagSuggestions []prompt.Suggest
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			flagSuggestions = append(flagSuggestions, prompt.Suggest{
				//Adding the -- to allow auto-complete to work on the flags flawlessly
				Text:        "--" + flag.Name,
				Description: flag.Usage,
			})
		})

		return prompt.FilterHasPrefix(flagSuggestions, word, true)
	}

	suggest, ok := cmd.Annotations["prompt_suggest"]
	if ok {
		switch suggest {
		case "user", "chat", "channel":
			return r.getPeerSuggestions(cmd.Context(), word, suggest)

		default:
		}
	}

	var promptSuggestions []prompt.Suggest
	if cmd.HasAvailableSubCommands() {
		for _, subCmd := range cmd.Commands() {
			promptSuggestions = append(promptSuggestions, prompt.Suggest{
				Text:        subCmd.Name(),
				Description: subCmd.Short,
			})
		}
	}

	return prompt.FilterHasPrefix(promptSuggestions, word, true)
}

func (r *Root) newPromptCmd(rootCmd *cobra.Command) *cobra.Command {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.AddCommand(&cobra.Command{
		Use:     "exit",
		Aliases: []string{"quit"},
		Short:   "Exit the prompt",
		Long:    `Exit the prompt`,
		Run: func(cmd *cobra.Command, args []string) {
			r.renderBye()
			os.Exit(0)
		},
	})

	promptCmd := &cobra.Command{
		Use:   "prompt",
		Short: "Start an interactive prompt",
		Long:  `Start an interactive prompt`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Open prompt with autocompletion")
			p := prompt.New(
				func(in string) {
					promptArgs := strings.Fields(in)
					os.Args = append([]string{os.Args[0]}, promptArgs...)

					r.cmdRoot.ExecuteContext(cmd.Context())
				},
				r.promptCompleter,
				prompt.OptionPrefix(">> "),
				prompt.OptionTitle("tgdownloader"),
			)

			p.Run()
		},
	}

	return promptCmd
}
