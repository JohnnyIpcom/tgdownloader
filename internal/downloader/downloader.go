package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
)

type settings struct {
	numWorkers int
	tracker    Tracker
	rewrite    bool
	dryRun     bool
	retryCount int
	retryDelay time.Duration
	onComplete func(Stats)
}

func (s *settings) setDefaults() {
	s.numWorkers = runtime.NumCPU()
	s.tracker = NewNullTracker()
	s.rewrite = false
	s.dryRun = false
	s.retryCount = 3
	s.retryDelay = 400 * time.Millisecond
}

type Option func(*settings)

func WithNumWorkers(numWorkers int) Option {
	return func(s *settings) {
		s.numWorkers = numWorkers
	}
}

func WithRewrite(rewrite bool) Option {
	return func(s *settings) {
		s.rewrite = rewrite
	}
}

func WithDryRun(dryRun bool) Option {
	return func(s *settings) {
		s.dryRun = dryRun
	}
}

func WithTracker(tracker Tracker) Option {
	return func(s *settings) {
		s.tracker = tracker
	}
}

func WithRetry(count int, delay time.Duration) Option {
	return func(s *settings) {
		if count > 0 {
			s.retryCount = count
		}

		if delay > 0 {
			s.retryDelay = delay
		}
	}
}

func WithOnComplete(fn func(Stats)) Option {
	return func(s *settings) {
		s.onComplete = fn
	}
}

// Pool is a pool of workers that download files
type Downloader struct {
	fs      afero.Fs
	service telegram.FileService

	outputDir     string
	numWorkers    int
	tracker       Tracker
	rewrite       bool
	dryRun        bool
	retryCount    int
	retryDelay    time.Duration
	pathClaims    map[string]string
	manifest      fileManifest
	manifestDirty bool
	onComplete    func(Stats)

	files   chan File
	queueWG sync.WaitGroup
	workerG *errgroup.Group

	errMu       sync.Mutex
	downloadErr error

	downloaded int64
	skipped    int64
	failed     int64
}

type Stats struct {
	Downloaded int64
	Skipped    int64
	Failed     int64
}

// NewDownloader creates a new pool of workers.
func New(fs afero.Fs, service telegram.FileService, opts ...Option) *Downloader {
	s := settings{}
	s.setDefaults()

	for _, opt := range opts {
		opt(&s)
	}

	return &Downloader{
		numWorkers: s.numWorkers,
		tracker:    s.tracker,
		rewrite:    s.rewrite,
		dryRun:     s.dryRun,
		retryCount: s.retryCount,
		retryDelay: s.retryDelay,
		pathClaims: make(map[string]string),
		manifest:   newFileManifest(),
		onComplete: s.onComplete,

		fs:      fs,
		files:   make(chan File),
		service: service,
	}
}

// SetOutputDir sets the output directory.
func (p *Downloader) SetOutputDir(dir string) {
	p.outputDir = dir
	p.createDirectoryIfNotExists(dir)

	manifest, err := loadFileManifest(p.fs, path.Join(dir, fileManifestName))
	if err != nil {
		p.recordError(err)
		return
	}
	p.manifest = manifest
	for _, identities := range manifest.Paths {
		for identity, actualPath := range identities {
			if actualPath, ok := cleanManifestPath(actualPath); ok {
				p.pathClaims[path.Join(dir, actualPath)] = identity
			}
		}
	}
}

// Start starts the pool of workers.
func (d *Downloader) Start(ctx context.Context) {
	log := logr.FromContextOrDiscard(ctx).WithName("downloader")
	log.Info("Downloader started", "workers", d.numWorkers)

	d.workerG, ctx = errgroup.WithContext(ctx)
	for i := 0; i < d.numWorkers; i++ {
		func(i int) {
			d.workerG.Go(func() error {
				return d.worker(ctx, log.WithName(fmt.Sprintf("worker-%d", i)))
			})
		}(i)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

type resumeFileService interface {
	DownloadFromOffset(ctx context.Context, file telegram.File, out io.Writer, offset int64) (int64, error)
}

// worker is a worker that downloads files.
func (d *Downloader) worker(ctx context.Context, log logr.Logger) error {
	defer log.Info("worker stopped")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case f, ok := <-d.files:
			if !ok {
				log.Info("no more jobs")
				return nil
			}

			log.Info("found job", "file", f.String())
			if err := d.downloadFile(ctx, f, log); err != nil {
				atomic.AddInt64(&d.failed, 1)
				d.recordError(err)
			}
		}
	}
}

func (d *Downloader) Stats() Stats {
	return Stats{
		Downloaded: atomic.LoadInt64(&d.downloaded),
		Skipped:    atomic.LoadInt64(&d.skipped),
		Failed:     atomic.LoadInt64(&d.failed),
	}
}

// Stop stops the pool of workers and waits for them to finish.
func (p *Downloader) Stop(ctx context.Context) error {
	p.queueWG.Wait()
	if p.manifestDirty {
		if err := saveFileManifest(p.fs, path.Join(p.outputDir, fileManifestName), p.manifest); err != nil {
			p.recordError(err)
		}
	}

	close(p.files)
	if err := p.workerG.Wait(); err != nil {
		p.recordError(err)
	}
	if p.onComplete != nil {
		p.onComplete(p.Stats())
	}

	p.tracker.WaitAndStop(ctx)

	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.downloadErr
}

func (d *Downloader) recordError(err error) {
	if err == nil {
		return
	}

	d.errMu.Lock()
	defer d.errMu.Unlock()

	if d.downloadErr == nil {
		d.downloadErr = err
		return
	}

	d.downloadErr = errors.Join(d.downloadErr, err)
}

// AddDownloadQueue adds a channel of files to the download queue.
func (p *Downloader) AddDownloadQueue(ctx context.Context, files <-chan File) {
	p.queueWG.Add(1)
	go func() {
		defer p.queueWG.Done()

		for {
			select {
			case <-ctx.Done():
				return

			case file, ok := <-files:
				if !ok {
					return
				}

				p.files <- p.reserveOutputPaths(file)
			}
		}
	}()
}

func (p *Downloader) reserveOutputPaths(file File) File {
	paths := make([]string, 0, len(file.subdirs)+1)
	for _, subdir := range file.subdirs {
		paths = append(paths, path.Join(p.outputDir, subdir, file.Name()))
	}
	if len(paths) == 0 {
		paths = append(paths, path.Join(p.outputDir, file.Name()))
	}

	identity := file.Identity()
	for i, outputPath := range paths {
		logicalPath := p.relativeOutputPath(outputPath)
		if actualPath, ok := p.manifest.lookup(logicalPath, identity); ok {
			if actualPath, valid := cleanManifestPath(actualPath); valid {
				paths[i] = path.Join(p.outputDir, actualPath)
				p.pathClaims[paths[i]] = identity
				continue
			}
		}

		claimedBy, claimed := p.pathClaims[outputPath]
		if !claimed && p.hasAmbiguousExistingFile(outputPath, file) {
			paths[i] = p.claimIdentifiedPath(outputPath, identity)
		} else if !claimed || claimedBy == identity {
			p.pathClaims[outputPath] = identity
			paths[i] = outputPath
		} else {
			paths[i] = p.claimIdentifiedPath(outputPath, identity)
		}

		if !p.dryRun {
			p.manifest.assign(logicalPath, identity, p.relativeOutputPath(paths[i]))
			p.manifestDirty = true
		}
	}

	file.outputPaths = paths
	return file
}

func (p *Downloader) relativeOutputPath(filename string) string {
	outputDir := strings.TrimSuffix(path.Clean(p.outputDir), "/")
	return strings.TrimPrefix(path.Clean(filename), outputDir+"/")
}

func (p *Downloader) hasAmbiguousExistingFile(outputPath string, file File) bool {
	if _, stable := file.StableIdentity(); !stable || file.Size() <= 0 {
		return false
	}

	info, err := p.fs.Stat(outputPath)
	return err == nil && info.Size() != file.Size()
}

func (p *Downloader) claimIdentifiedPath(outputPath, identity string) string {
	candidate := pathWithIdentity(outputPath, identity)
	for suffix := 2; ; suffix++ {
		candidateClaim, candidateClaimed := p.pathClaims[candidate]
		if !candidateClaimed || candidateClaim == identity {
			p.pathClaims[candidate] = identity
			return candidate
		}
		candidate = pathWithIdentity(outputPath, fmt.Sprintf("%s-%d", identity, suffix))
	}
}

func pathWithIdentity(filename, identity string) string {
	ext := path.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	return base + "_" + identity + ext
}

// downloadFile downloads a file.
func (p *Downloader) downloadFile(ctx context.Context, file File, log logr.Logger) error {
	outputPaths := file.outputPaths
	if len(outputPaths) == 0 {
		file = p.reserveOutputPaths(file)
		outputPaths = file.outputPaths
	}

	for _, outputPath := range outputPaths {
		outputDir := path.Dir(outputPath)
		if err := p.createDirectoryIfNotExists(outputDir); err != nil {
			log.Error(err, "failed to create directory", "directory", outputDir)
			return apperr.New("downloader.create_directory", apperr.KindIO, fmt.Errorf("create output directory %q: %w", outputDir, err))
		}
	}

	if resumeSvc, ok := p.service.(resumeFileService); ok {
		resumed, resumeErr := p.resumeExistingPartialFile(ctx, file, outputPaths, resumeSvc, log)
		if resumed {
			return resumeErr
		}
	}

	saver := NewAferoSaver(p.fs)
	if p.dryRun {
		saver = NewNullSaver()
	}

	for _, outputPath := range outputPaths {
		if err := p.addFileToSaver(saver, outputPath); err != nil {
			log.Error(err, "failed to add file to saver", "filename", file.Name())
			return apperr.New("downloader.prepare_output", apperr.KindIO, fmt.Errorf("prepare output file %q: %w", file.Name(), err))
		}
	}

	if !saver.IsValid() {
		log.Info("no valid files to write to")
		atomic.AddInt64(&p.skipped, 1)
		return nil
	}

	displayName := path.Base(outputPaths[0])
	writer := p.tracker.WrapWriter(saver, displayName, file.Size())

	var err error
	for attempt := 1; attempt <= p.retryCount; attempt++ {
		err = p.service.Download(ctx, file.File, writerFunc(func(p []byte) (int, error) {
			select {
			case <-ctx.Done():
				writer.Fail()
				return 0, ctx.Err()

			default:
			}

			return writer.Write(p)
		}))
		if err == nil {
			break
		}

		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}

		if attempt < p.retryCount {
			log.Info("download retry scheduled", "filename", file.Name(), "attempt", attempt+1, "max_attempts", p.retryCount)
			time.Sleep(p.retryDelay)
		}
	}

	if err != nil {
		writer.Fail()

		log.Error(err, "failed to download file", "filename", file.Name())
		if removeErr := saver.Remove(); removeErr != nil {
			return apperr.New("downloader.download", apperr.KindIO, errors.Join(
				fmt.Errorf("download file %q: %w", file.Name(), err),
				apperr.New("downloader.cleanup_failed_file", apperr.KindIO, fmt.Errorf("cleanup failed file %q: %w", file.Name(), removeErr)),
			))
		}

		return apperr.New("downloader.download", apperr.KindNetwork, fmt.Errorf("download file %q: %w", file.Name(), err))
	}

	saver.Close()
	writer.Done()
	atomic.AddInt64(&p.downloaded, 1)

	log.Info("downloaded document", "filename", file.Name())
	return nil
}

func (p *Downloader) resumeExistingPartialFile(
	ctx context.Context,
	file File,
	outputPaths []string,
	service resumeFileService,
	log logr.Logger,
) (bool, error) {
	if p.dryRun || p.rewrite || len(outputPaths) != 1 {
		return false, nil
	}

	targetPath := outputPaths[0]
	if file.Size() <= 0 {
		return false, nil
	}

	info, err := p.fs.Stat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return true, apperr.New("downloader.resume.stat", apperr.KindIO, fmt.Errorf("stat partial file %q: %w", targetPath, err))
	}

	currentOffset := info.Size()
	if currentOffset <= 0 || currentOffset >= file.Size() {
		return false, nil
	}

	log.Info("resuming partial telegram file", "filename", file.Name(), "path", targetPath, "offset", currentOffset, "size", file.Size())

	for attempt := 1; attempt <= p.retryCount; attempt++ {
		if err := ctx.Err(); err != nil {
			return true, err
		}

		info, statErr := p.fs.Stat(targetPath)
		if statErr != nil {
			return true, apperr.New("downloader.resume.stat", apperr.KindIO, fmt.Errorf("stat partial file %q: %w", targetPath, statErr))
		}

		currentOffset = info.Size()
		if currentOffset >= file.Size() {
			atomic.AddInt64(&p.downloaded, 1)
			log.Info("resume already complete", "filename", file.Name(), "path", targetPath)
			return true, nil
		}

		fileHandle, openErr := p.fs.OpenFile(targetPath, os.O_WRONLY|os.O_APPEND, 0644)
		if openErr != nil {
			return true, apperr.New("downloader.resume.open", apperr.KindIO, fmt.Errorf("open partial file %q: %w", targetPath, openErr))
		}

		remaining := file.Size() - currentOffset
		displayName := path.Base(outputPaths[0])
		writer := p.tracker.WrapWriter(fileHandle, displayName, remaining)

		resumeErr := func() error {
			_, err := service.DownloadFromOffset(ctx, file.File, writerFunc(func(data []byte) (int, error) {
				select {
				case <-ctx.Done():
					writer.Fail()
					return 0, ctx.Err()
				default:
				}

				return writer.Write(data)
			}), currentOffset)
			return err
		}()

		closeErr := fileHandle.Close()
		if closeErr != nil {
			writer.Fail()
			return true, apperr.New("downloader.resume.close", apperr.KindIO, fmt.Errorf("close partial file %q: %w", targetPath, closeErr))
		}

		if resumeErr == nil {
			writer.Done()

			finalInfo, finalErr := p.fs.Stat(targetPath)
			if finalErr != nil {
				return true, apperr.New("downloader.resume.final_stat", apperr.KindIO, fmt.Errorf("stat resumed file %q: %w", targetPath, finalErr))
			}

			if finalInfo.Size() < file.Size() {
				resumeErr = apperr.New("downloader.resume.incomplete", apperr.KindNetwork, fmt.Errorf("resume incomplete: got %d of %d bytes", finalInfo.Size(), file.Size()))
			} else {
				atomic.AddInt64(&p.downloaded, 1)
				log.Info("resumed telegram file", "filename", file.Name(), "path", targetPath, "size", finalInfo.Size())
				return true, nil
			}
		}

		writer.Fail()

		if ctx.Err() != nil {
			return true, ctx.Err()
		}

		if attempt < p.retryCount {
			log.Info("resume retry scheduled", "filename", file.Name(), "attempt", attempt+1, "max_attempts", p.retryCount)
			time.Sleep(p.retryDelay)
			continue
		}

		return true, apperr.New("downloader.resume", apperr.KindNetwork, fmt.Errorf("resume file %q from offset %d: %w", file.Name(), currentOffset, resumeErr))
	}

	return true, apperr.New("downloader.resume", apperr.KindNetwork, fmt.Errorf("resume file %q exceeded retry limit", file.Name()))
}

// addFileToSaver adds a file to the saver if it does not exist or if it should be rewritten.
func (p *Downloader) addFileToSaver(ms MultiSaver, filepath string) error {
	exists, err := afero.Exists(p.fs, filepath)
	if err != nil {
		return err
	}

	if exists && !p.rewrite {
		return nil
	}

	return ms.AddFile(filepath)
}

// createDirectoryIfNotExists creates a directory and all parent directories if it does not exist
func (p *Downloader) createDirectoryIfNotExists(dir string) error {
	if p.dryRun {
		return nil
	}

	ok, err := afero.DirExists(p.fs, dir)
	if err != nil {
		return err
	}

	if !ok {
		err = p.fs.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}
