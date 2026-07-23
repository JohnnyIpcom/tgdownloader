package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	telegrammocks "github.com/johnnyipcom/tgdownloader/pkg/telegram/mocks"
	"go.uber.org/mock/gomock"
)

func TestParseMessageLinkCommentRequiresBroadcastChannel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	commentPeer := telegrammocks.NewMocklinkedChatPeer(ctrl)
	commentPeer.EXPECT().IsBroadcast().Return(false)

	peerSvc := telegrammocks.NewMockPeerService(ctrl)
	peerSvc.EXPECT().Resolve(gomock.Any(), "example").Return(commentPeer, nil)

	client := &Client{}
	client.PeerService = peerSvc

	peer, msgID, err := client.ParseMessageLink(context.Background(), "https://t.me/example/4434?comment=360409")
	if err == nil {
		t.Fatalf("expected error, got nil with peer=%v msgID=%d", peer, msgID)
	}

	if !strings.Contains(err.Error(), "broadcast channel") {
		t.Fatalf("expected broadcast channel error, got %v", err)
	}
}

type recordingProgress struct {
	trackers []string
	done     []string
	failed   []string
	waited   int
}

func (p *recordingProgress) Tracker(message string) Tracker {
	p.trackers = append(p.trackers, message)
	return &recordingTracker{message: message, progress: p}
}

func (p *recordingProgress) Wait(ctx context.Context) {
	p.waited++
}

func (p *recordingProgress) WaitAndStop(ctx context.Context) {}

type recordingTracker struct {
	message  string
	progress *recordingProgress
}

func (t *recordingTracker) Fail() {
	t.progress.failed = append(t.progress.failed, t.message)
}

func (t *recordingTracker) Done() {
	t.progress.done = append(t.progress.done, t.message)
}

func TestClientFinishConnectingTracksDone(t *testing.T) {
	progress := &recordingProgress{}
	client := &Client{progress: progress}
	connectTracker := progress.Tracker("Connecting")

	client.finishConnecting(context.Background(), connectTracker)

	if len(progress.done) != 1 || progress.done[0] != "Connecting" {
		t.Fatalf("expected Connecting done, got %v", progress.done)
	}
	if len(progress.failed) != 0 {
		t.Fatalf("expected no failed trackers, got %v", progress.failed)
	}
	if progress.waited != 1 {
		t.Fatalf("expected one wait after Connecting done, got %d", progress.waited)
	}
}

func TestClientWaitForConnectInitDoesNotFinishConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errC := make(chan error, 1)
	initDone := make(chan struct{})
	close(initDone)

	progress := &recordingProgress{}
	client := &Client{progress: progress}
	connectTracker := progress.Tracker("Connecting")

	stop, err := client.waitForConnectInit(ctx, cancel, errC, initDone, connectTracker)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	errC <- nil
	if err := stop(); err != nil {
		t.Fatalf("expected stop without error, got %v", err)
	}

	if len(progress.trackers) != 1 || progress.trackers[0] != "Connecting" {
		t.Fatalf("expected Connecting tracker, got %v", progress.trackers)
	}
	if len(progress.done) != 0 {
		t.Fatalf("expected no done trackers, got %v", progress.done)
	}
	if len(progress.failed) != 0 {
		t.Fatalf("expected no failed trackers, got %v", progress.failed)
	}
	if progress.waited != 0 {
		t.Fatalf("expected no wait, got %d", progress.waited)
	}
}

func TestClientWaitForConnectInitFailsConnectingOnError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wantErr := errors.New("connect failed")
	errC := make(chan error, 1)
	errC <- wantErr
	initDone := make(chan struct{})

	progress := &recordingProgress{}
	client := &Client{progress: progress}
	connectTracker := progress.Tracker("Connecting")

	_, err := client.waitForConnectInit(ctx, cancel, errC, initDone, connectTracker)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}

	if len(progress.failed) != 1 || progress.failed[0] != "Connecting" {
		t.Fatalf("expected Connecting failed, got %v", progress.failed)
	}
	if len(progress.done) != 0 {
		t.Fatalf("expected no done trackers, got %v", progress.done)
	}
	if progress.waited != 1 {
		t.Fatalf("expected one wait after Connecting failed, got %d", progress.waited)
	}
}
