package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	prompt "github.com/c-bata/go-prompt"
	"github.com/go-andiamo/splitter"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func (r *Root) getPeerSuggestions(ctx context.Context, word string, peerType string) []prompt.Suggest {
	var filter telegram.CachedPeerFilter
	switch peerType {
	case "user":
		filter = telegram.OnlyUsersCachedPeerFilter()
	case "chat":
		filter = telegram.OnlyChatsCachedPeerFilter()
	case "channel":
		filter = telegram.OnlyChannelsCachedPeerFilter()
	case "chatorchannel":
		filter = telegram.OrCachedPeerFilter(
			telegram.OnlyChatsCachedPeerFilter(),
			telegram.OnlyChannelsCachedPeerFilter(),
		)
	case "any":
		filter = telegram.OrCachedPeerFilter(
			telegram.OnlyUsersCachedPeerFilter(),
			telegram.OnlyChatsCachedPeerFilter(),
			telegram.OnlyChannelsCachedPeerFilter(),
		)
	}

	peers, err := r.client.CacheService.GetCachedPeers(ctx, filter)
	if err != nil {
		return []prompt.Suggest{}
	}

	var suggestions []prompt.Suggest
	for _, peer := range peers {
		suggestions = append(suggestions, prompt.Suggest{
			Text:        renderer.RenderTDLibPeerID(peer.TDLibPeerID()),
			Description: renderer.RenderName(peer.Name()),
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

func (r *Root) getDateSuggestions(word string, format string) []prompt.Suggest {
	dateNow := time.Now()
	dates := []prompt.Suggest{
		{Text: fmt.Sprintf("\"%s\"", dateNow.Format(format))},
		{Text: fmt.Sprintf("\"%s\"", dateNow.AddDate(0, 0, -1).Format(format))},
		{Text: fmt.Sprintf("\"%s\"", dateNow.AddDate(0, 0, -7).Format(format))},
		{Text: fmt.Sprintf("\"%s\"", dateNow.AddDate(0, 0, -30).Format(format))},
		{Text: fmt.Sprintf("\"%s\"", dateNow.AddDate(0, 0, -365).Format(format))},
	}

	return prompt.FilterHasPrefix(dates, word, true)
}

func (r *Root) newExecutor(rootCmd *cobra.Command) prompt.Executor {
	s := splitter.MustCreateSplitter(' ', splitter.DoubleQuotesDoubleEscaped)
	s.AddDefaultOptions(splitter.Trim(`/"`))

	return func(in string) {
		args, err := s.Split(in)
		if err != nil {
			renderer.RenderError(err)
			return
		}

		rootCmd.SetArgs(args)
		if err := rootCmd.ExecuteContext(rootCmd.Context()); err != nil {
			renderer.RenderError(err)
		}
	}
}

func (r *Root) newCompleter(rootCmd *cobra.Command) prompt.Completer {
	return func(d prompt.Document) []prompt.Suggest {
		args := strings.Fields(d.CurrentLine())
		word := d.GetWordBeforeCursor()

		currCmd := rootCmd
		if found, _, err := rootCmd.Find(args); err == nil {
			currCmd = found
		}

		if strings.HasPrefix(word, "-") {
			var flagSuggestions []prompt.Suggest
			currCmd.Flags().VisitAll(func(flag *pflag.Flag) {
				flagSuggestions = append(flagSuggestions, prompt.Suggest{
					//Adding the -- to allow auto-complete to work on the flags flawlessly
					Text:        "--" + flag.Name,
					Description: flag.Usage,
				})
			})

			return prompt.FilterHasPrefix(flagSuggestions, word, true)
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

		case "--limit":
			return prompt.FilterHasPrefix([]prompt.Suggest{
				{Text: "10"},
				{Text: "20"},
				{Text: "50"},
				{Text: "100"},
			}, word, true)

		case "--offset-date":
			return r.getDateSuggestions(word, "2006-01-02 15:04:05")

		default:
			if strings.HasPrefix(lastArg, "--") {
				return []prompt.Suggest{}
			}
		}

		suggest, ok := currCmd.Annotations["prompt_suggest"]
		if ok {
			switch suggest {
			case "user", "chat", "channel", "chatorchannel", "any":
				return r.getPeerSuggestions(currCmd.Context(), word, suggest)

			default:
			}
		}

		var promptSuggestions []prompt.Suggest
		if currCmd.HasAvailableSubCommands() {
			for _, subCmd := range currCmd.Commands() {
				promptSuggestions = append(promptSuggestions, prompt.Suggest{
					Text:        subCmd.Name(),
					Description: subCmd.Short,
				})
			}
		}

		return prompt.FilterHasPrefix(promptSuggestions, word, true)
	}
}

func (r *Root) newPromptCmd(rootCmd *cobra.Command) *cobra.Command {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.DisableSuggestions = true

	rootCmd.AddCommand(&cobra.Command{
		Use:     "exit",
		Aliases: []string{"quit"},
		Short:   "Exit the prompt",
		Long:    `Exit the prompt`,
		Run: func(cmd *cobra.Command, args []string) {
			r.stop()
			renderer.RenderBye()

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
				r.newExecutor(rootCmd),
				r.newCompleter(rootCmd),
				prompt.OptionPrefix(">> "),
				prompt.OptionTitle("tgdownloader"),
			)

			p.Run()
		},
	}

	return promptCmd
}
