package telegram

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type File struct {
	name     string
	size     int64
	dc       int
	location tg.InputFileLocationClass
	metadata map[string]interface{}
}

func (f File) String() string {
	return fmt.Sprintf("File{dc: %d, name: %s, size: %d}", f.dc, f.name, f.size)
}

func (f File) DC() int {
	return f.dc
}

func (f File) Name() string {
	return f.name
}

func (f File) Size() int64 {
	return f.size
}

func (f File) Metadata() map[string]interface{} {
	return f.metadata
}

type FileService interface {
	GetAllFiles(ctx context.Context, peer peers.Peer, opts ...GetAllFilesOption) (<-chan File, error)
	GetAllFilesFromNewMessages(ctx context.Context, peer peers.Peer, opts ...GetAllFilesOption) (<-chan File, error)
	GetFilesFromMessage(ctx context.Context, peer peers.Peer, msgID int, opts ...GetFileOption) ([]*File, error)
	GetFilesFromGroupedMessage(ctx context.Context, peer peers.Peer, msg *tg.Message) ([]*File, error)
	Download(ctx context.Context, file File, out io.Writer) error
}

type fileService service

var _ FileService = (*fileService)(nil)

type getAllFilesOption struct {
	userID     int64
	limit      int
	offsetDate int
}

type GetAllFilesOption interface {
	apply(*getAllFilesOption) error
}

type getfileUserIDOption struct {
	userID int64
}

func (o getfileUserIDOption) apply(opts *getAllFilesOption) error {
	opts.userID = o.userID
	return nil
}

func GetFileWithUserID(userID int64) GetAllFilesOption {
	return getfileUserIDOption{userID: userID}
}

type getfileLimitOption struct {
	limit int
}

func (o getfileLimitOption) apply(opts *getAllFilesOption) error {
	opts.limit = o.limit
	return nil
}

func GetFileWithLimit(limit int) GetAllFilesOption {
	return getfileLimitOption{limit: limit}
}

type getfileOffsetDateOption struct {
	offsetDate int
}

func (o getfileOffsetDateOption) apply(opts *getAllFilesOption) error {
	opts.offsetDate = o.offsetDate
	return nil
}

func GetFileWithOffsetDate(offsetDate int) GetAllFilesOption {
	return getfileOffsetDateOption{offsetDate: offsetDate}
}

type getFileOption struct {
	grouped bool
}

type GetFileOption interface {
	apply(*getFileOption) error
}

type getFileWithGrouped struct {
	grouped bool
}

func (o getFileWithGrouped) apply(opts *getFileOption) error {
	opts.grouped = o.grouped
	return nil
}

func GetFileWithGrouped(grouped bool) GetFileOption {
	return getFileWithGrouped{grouped: grouped}
}

func (s *fileService) extractFilesFromMessageElem(ctx context.Context, elem messages.Elem) ([]*File, int64, error) {
	files, err := getFilesFromMessageElem(elem)
	if err != nil {
		return nil, 0, err
	}

	var peer peers.Peer
	fromID, ok := elem.Msg.GetFromID()
	if ok {
		from, err := s.client.ExtractPeer(ctx, elem.Entities, fromID)
		if err != nil {
			return nil, 0, apperr.New("telegram.file.extract_files.extract_from", apperr.KindInternal, fmt.Errorf("extract fromID: %w", err))
		}

		peer = from
	}

	if peer == nil {
		p, err := s.client.ExtractPeer(ctx, elem.Entities, elem.Msg.GetPeerID())
		if err != nil {
			return nil, 0, apperr.New("telegram.file.extract_files.extract_peer", apperr.KindInternal, fmt.Errorf("extract peer: %w", err))
		}

		peer = p
	}

	visibleName := peer.VisibleName()

	msg, ok := elem.Msg.(*tg.Message)
	hashtags := []string{}
	if ok {
		hashtags = extractHashtags(msg.GetMessage())
	}

	for _, file := range files {
		if file == nil {
			continue
		}

		file.metadata["peername"] = strconv.FormatInt(peer.ID(), 10)
		if visibleName != "" {
			file.metadata["peername"] = visibleName
		}

		if len(hashtags) > 0 {
			file.metadata["hashtags"] = hashtags
		}
	}

	return files, peer.ID(), nil
}

// GetAllFiles returns channel for file IDs and error channel.
func (s *fileService) GetAllFiles(ctx context.Context, peer peers.Peer, opts ...GetAllFilesOption) (<-chan File, error) {
	options := getAllFilesOption{
		limit: int(^uint(0) >> 1), // MaxInt
	}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return nil, apperr.Wrap("telegram.file.get_all_files.options", err)
		}
	}

	var fileCounter int64
	fileChan := make(chan File)
	go func() {
		defer close(fileChan)

		queryBuilder := query.Messages(s.client.API()).GetHistory(peer.InputPeer())
		queryBuilder = queryBuilder.OffsetDate(options.offsetDate)
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
				s.logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
				return errLimitReached
			}

			files, peerID, err := s.extractFilesFromMessageElem(ctx, elem)
			if err != nil {
				if errors.Is(err, errNoFilesInMessage) || errors.Is(err, errPaidMediaLocked) {
					return nil
				}

				return err
			}

			if len(files) == 0 || (options.userID > 0 && peerID != options.userID) {
				return nil
			}

			for _, file := range files {
				if file == nil {
					continue
				}

				if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
					s.logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
					return errLimitReached
				}

				select {
				case fileChan <- *file:
					atomic.AddInt64(&fileCounter, 1)

				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		}); err != nil {
			if !errors.Is(err, errLimitReached) {
				s.logger.Error("failed to get files", zap.Error(err))
				return
			}

			return
		}
	}()

	return fileChan, nil
}

// GetAllFilesFromNewMessages returns files from new messages.
func (s *fileService) GetAllFilesFromNewMessages(ctx context.Context, p peers.Peer, opts ...GetAllFilesOption) (<-chan File, error) {
	options := getAllFilesOption{
		limit: int(^uint(0) >> 1), // MaxInt
	}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return nil, apperr.Wrap("telegram.file.get_new_files.options", err)
		}
	}

	var fileCounter int64

	fileChan := make(chan File)

	onNewMessage := func(ctx context.Context, e tg.Entities, msg tg.MessageClass) error {
		if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
			s.logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
			return errLimitReached
		}

		nonEmpty, ok := msg.AsNotEmpty()
		if !ok {
			s.logger.Debug("empty message")
			return nil
		}

		entities := peer.EntitiesFromUpdate(e)

		msgPeer, err := entities.ExtractPeer(nonEmpty.GetPeerID())
		if err != nil {
			msgPeer = &tg.InputPeerEmpty{}
		}

		files, peerID, err := s.extractFilesFromMessageElem(ctx, messages.Elem{
			Msg:      nonEmpty,
			Peer:     msgPeer,
			Entities: entities,
		})

		if err != nil {
			if errors.Is(err, errNoFilesInMessage) || errors.Is(err, errPaidMediaLocked) {
				return nil
			}

			return err
		}

		if len(files) == 0 || peerID != p.ID() {
			return nil
		}

		for _, file := range files {
			if file == nil {
				continue
			}

			if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
				s.logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
				return errLimitReached
			}

			select {
			case fileChan <- *file:
				atomic.AddInt64(&fileCounter, 1)

			case <-ctx.Done():
				return ctx.Err()
			}
		}

		return nil
	}

	s.client.dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		return onNewMessage(ctx, e, update.Message)
	})

	s.client.dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		return onNewMessage(ctx, e, update.Message)
	})

	return fileChan, nil
}

func (s *fileService) GetFilesFromMessage(ctx context.Context, peer peers.Peer, msgID int, opts ...GetFileOption) ([]*File, error) {
	options := getFileOption{
		grouped: true,
	}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return nil, apperr.Wrap("telegram.file.get_message_files.options", err)
		}
	}

	queryBuilder := query.Messages(s.client.API()).GetHistory(peer.InputPeer())
	queryBuilder = queryBuilder.OffsetID(msgID + 1).BatchSize(1)

	iter := queryBuilder.Iter()
	if !iter.Next(ctx) {
		return nil, apperr.Wrap("telegram.file.get_message_files.iter", iter.Err())
	}

	elem := iter.Value()

	msg, ok := elem.Msg.(*tg.Message)
	if !ok {
		return nil, apperr.New("telegram.file.get_message_files.type", apperr.KindInternal, errors.New("not a message"))
	}

	if _, ok := msg.GetGroupedID(); ok && options.grouped {
		files, err := s.GetFilesFromGroupedMessage(ctx, peer, msg)
		if err != nil && !errors.Is(err, errNoFilesInMessage) {
			return nil, err
		}

		return files, nil
	}

	files, _, err := s.extractFilesFromMessageElem(ctx, iter.Value())
	return files, err
}

func (s *fileService) GetFilesFromGroupedMessage(ctx context.Context, peer peers.Peer, msg *tg.Message) ([]*File, error) {
	group, ok := msg.GetGroupedID()
	if !ok {
		return nil, apperr.New("telegram.file.get_grouped_files.group", apperr.KindConfig, errors.New("not grouped message"))
	}

	batchSize := 20

	queryBuilder := query.Messages(s.client.API()).GetHistory(peer.InputPeer())
	queryBuilder = queryBuilder.OffsetID(msg.ID + 11).BatchSize(batchSize) // 10 messages before and 10 after

	files := make([]*File, 0, batchSize)

	iter := queryBuilder.Iter()
	for i := 0; iter.Next(ctx) && i < batchSize; i++ {
		m, ok := iter.Value().Msg.(*tg.Message)
		if !ok {
			continue
		}

		groupID, ok := m.GetGroupedID()
		if !ok {
			continue
		}

		if groupID != group {
			continue
		}

		messageFiles, _, err := s.extractFilesFromMessageElem(ctx, iter.Value())
		if err != nil {
			if errors.Is(err, errNoFilesInMessage) || errors.Is(err, errPaidMediaLocked) {
				continue
			}

			return nil, err
		}

		for _, file := range messageFiles {
			if file == nil {
				continue
			}

			files = append(files, file)
		}
	}

	return files, nil
}

func (s *fileService) Download(ctx context.Context, file File, out io.Writer) error {
	builder := s.client.client.Download(file.location)
	_, err := builder.Stream(ctx, out)
	if err != nil {
		return apperr.New("telegram.file.download", apperr.KindNetwork, err)
	}

	return nil
}
