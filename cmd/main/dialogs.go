package cmd

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

type filterDialogFunc func(telegram.DialogInfo) bool

func renderDialogsTable(dialogs []telegram.DialogInfo, filterFuncs ...filterDialogFunc) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetAutoIndex(true)
	t.AppendHeader(
		table.Row{
			"Name",
			"ID",
			"Type",
		},
	)

	t.SortBy([]table.SortBy{
		{Name: "Name", Mode: table.Asc},
	})

	for _, dialog := range dialogs {
		if dialog.Err() != nil {
			continue
		}

		skip := false
		if len(filterFuncs) > 0 {
			for _, filterFunc := range filterFuncs {
				if !filterFunc(dialog) {
					skip = true
					break
				}
			}
		}

		if !skip {
			t.AppendRow(
				table.Row{
					dialog.Peer.Name,
					dialog.Peer.ID,
					dialog.Peer.Type.String(),
				},
			)
		}
	}

	t.Render()
}

func renderDialogsTableAsync(ctx context.Context, d <-chan telegram.DialogInfo, total int, filterFunc ...filterDialogFunc) error {
	pw := progress.NewWriter()
	pw.SetAutoStop(true)
	pw.SetTrackerLength(25)
	pw.SetTrackerPosition(progress.PositionRight)
	pw.SetSortBy(progress.SortByPercentDsc)
	pw.SetStyle(progress.StyleDefault)
	pw.SetUpdateFrequency(time.Millisecond * 100)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.PercentFormat = "%4.1f%%"
	pw.Style().Visibility.ETA = true
	pw.Style().Visibility.ETAOverall = true

	go pw.Render()

	tracker := &progress.Tracker{
		Total:   int64(total),
		Message: "Fetching dialogs",
		Units:   progress.UnitsDefault,
	}

	pw.AppendTracker(tracker)
	var dialogs []telegram.DialogInfo

	defer func() {
		for pw.IsRenderInProgress() {
			time.Sleep(time.Millisecond)
		}

		renderDialogsTable(dialogs, filterFunc...)
	}()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case dialog, ok := <-d:
				if !ok {
					return nil
				}

				tracker.Increment(1)
				dialogs = append(dialogs, dialog)
			}
		}
	})

	if err := g.Wait(); err != nil {
		tracker.MarkAsErrored()
		return err
	}

	tracker.MarkAsDone()
	return nil
}

func newDialogsCmd(ctx context.Context, r *Root) *cobra.Command {
	dialogCmd := &cobra.Command{
		Use:   "dialog",
		Short: "Manage dialogs",
		Long:  "Manage dialogs",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.HelpFunc()(cmd, []string{})
		},
	}

	dialogListCmd := &cobra.Command{
		Use:   "list",
		Short: "List dialogs",
		Long:  "List dialogs",
		RunE: func(cmd *cobra.Command, args []string) error {
			filter, err := cmd.Flags().GetString("type")
			if err != nil {
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, client telegram.Client) error {
				dialogs, total, err := client.GetAllDialogs(ctx)
				if err != nil {
					return err
				}

				filterFuncs := []filterDialogFunc{}
				if filter != "" {
					filterFuncs = append(filterFuncs, func(dialog telegram.DialogInfo) bool {
						return strings.EqualFold(dialog.Peer.Type.String(), filter)
					})
				}

				return renderDialogsTableAsync(ctx, dialogs, total, filterFuncs...)
			})
		},
	}

	dialogListCmd.Flags().StringP("type", "t", "", "Filter by type(channel, chat, user)")

	dialogCmd.AddCommand(dialogListCmd)
	return dialogCmd
}
