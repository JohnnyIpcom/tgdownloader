package downloader

import (
	"context"
	"fmt"
	"path"
	"runtime"
	"sync"

	"github.com/go-logr/logr"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
)

type settings struct {
	numWorkers int
	tracker    Tracker
	rewrite    bool
	dryRun     bool
}

func (s *settings) setDefaults() {
	s.numWorkers = runtime.NumCPU()
	s.tracker = NewNullTracker()
	s.rewrite = false
	s.dryRun = false
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

// Pool is a pool of workers that download files
type Downloader struct {
	fs      afero.Fs
	service telegram.FileService

	outputDir  string
	numWorkers int
	tracker    Tracker
	rewrite    bool
	dryRun     bool

	files   chan File
	queueWG sync.WaitGroup
	workerG *errgroup.Group
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

		fs:      fs,
		files:   make(chan File),
		service: service,
	}
}

// SetOutputDir sets the output directory.
func (p *Downloader) SetOutputDir(dir string) {
	p.outputDir = dir
	p.createDirectoryIfNotExists(dir)
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
			d.downloadFile(ctx, f, log)
		}
	}
}

// Stop stops the pool of workers and waits for them to finish.
func (p *Downloader) Stop(ctx context.Context) error {
	p.queueWG.Wait()

	close(p.files)
	p.workerG.Wait()

	p.tracker.WaitAndStop(ctx)
	return nil
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

				p.files <- file
			}
		}
	}()
}

// downloadFile downloads a file.
func (p *Downloader) downloadFile(ctx context.Context, file File, log logr.Logger) {
	saver := NewAferoSaver(p.fs)
	if p.dryRun {
		saver = NewNullSaver()
	}

	for _, subdir := range file.subdirs {
		outputDir := path.Join(p.outputDir, subdir)
		if err := p.createDirectoryIfNotExists(outputDir); err != nil {
			log.Error(err, "failed to create directory", "directory", outputDir)
			return
		}

		if err := p.addFileToSaver(saver, path.Join(outputDir, file.Name())); err != nil {
			log.Error(err, "failed to add file to saver", "filename", file.Name())
			return
		}
	}

	if !saver.IsValid() {
		if err := p.addFileToSaver(saver, path.Join(p.outputDir, file.Name())); err != nil {
			log.Error(err, "failed to add file to saver", "filename", file.Name())
			return
		}
	}

	if !saver.IsValid() {
		log.Info("no valid files to write to")
		return
	}

	writer := p.tracker.WrapWriter(saver, file.Name(), file.Size())
	if err := p.service.Download(ctx, file.File, writerFunc(func(p []byte) (int, error) {
		select {
		case <-ctx.Done():
			writer.Fail()
			return 0, ctx.Err()

		default:
		}

		return writer.Write(p)
	})); err != nil {
		writer.Fail()

		log.Error(err, "failed to download file", "filename", file.Name())
		saver.Remove()
	}

	saver.Close()
	writer.Done()

	log.Info("downloaded document", "filename", file.Name())
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
