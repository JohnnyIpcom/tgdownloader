package renderer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
)

func TestRenderErrorPreservesStructuredLevelAndOrder(t *testing.T) {
	events := make(chan Event, 8)
	writer := NewEventWriter(NewChannelEventSink(events))
	fmt.Fprint(writer, "pending")
	RenderErrorConcise(writer, errors.New("first\nsecond"))
	RenderError(writer, context.Canceled)
	fmt.Fprintln(writer, "normal")
	writer.Flush()
	close(events)

	var got []Event
	for event := range events {
		got = append(got, event)
	}

	want := []Event{
		{Kind: EventLine, Text: "pending"},
		{Kind: EventLine, Level: LineError, Text: "Error: first"},
		{Kind: EventLine, Level: LineError, Text: "second"},
		{Kind: EventLine, Level: LineWarning, Text: "Interrupted"},
		{Kind: EventLine, Text: "normal"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestRenderErrorsToPlainWriterHaveNoANSI(t *testing.T) {
	var output strings.Builder
	err := apperr.New("download.test", apperr.KindNetwork, errors.New("failed"))
	RenderError(&output, err)
	RenderErrorConcise(&output, err)
	RenderErrorConcise(&output, context.Canceled)
	RenderError(&output, nil)
	RenderErrorConcise(&output, nil)

	want := fmt.Sprintf("Error (%s) at download.test: failed\nError: failed\nInterrupted\n", apperr.KindNetwork)
	if output.String() != want {
		t.Fatalf("plain output = %q, want %q", output.String(), want)
	}
}
