package downloader

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
)

type saveFileOption struct {
	saveByHashtags bool
	saveOnlyIfNew  bool
}

type SaveFileOption func(*saveFileOption)

func SaveByHashtags() SaveFileOption {
	return func(o *saveFileOption) {
		o.saveByHashtags = true
	}
}

func SaveOnlyIfNew() SaveFileOption {
	return func(o *saveFileOption) {
		o.saveOnlyIfNew = true
	}
}

// Pool is a pool of workers that download files
type Downloader struct {
	outputDir string
	fs        afero.Fs
	files     chan FileInfo
	renderer  *renderer.DownloadRenderer
	service   telegram.FileService
	queueWG   sync.WaitGroup
	workerG   *errgroup.Group
	workers   int
}

// NewDownloader creates a new pool of workers.
func NewDownloader(fs afero.Fs, workers int, service telegram.FileService) *Downloader {
	return &Downloader{
		fs:       fs,
		files:    make(chan FileInfo),
		renderer: renderer.NewDownloadRenderer(renderer.WithNumTrackersExpected(workers)),
		service:  service,
		workers:  workers,
	}
}

// SetOutputDir sets the output directory.
func (p *Downloader) SetOutputDir(dir string) {
	p.outputDir = dir
}

// Start starts the pool of workers.
func (d *Downloader) Start(ctx context.Context) {
	log := logr.FromContextOrDiscard(ctx).WithName("downloader")
	log.Info("Downloader started", "workers", d.workers)

	d.workerG, ctx = errgroup.WithContext(ctx)
	for i := 0; i < d.workers; i++ {
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
			saver, err := d.createSaver(f)
			if err != nil {
				log.Error(err, "failed to create file", "filename", f.Filename())
				continue
			}

			if !saver.IsValid() {
				log.Info("no valid files to write to")
				continue
			}

			writer := d.renderer.TrackedWriter(f.Filename(), f.Size(), saver)
			if err := d.service.Download(ctx, f.File, writerFunc(func(p []byte) (int, error) {
				select {
				case <-ctx.Done():
					writer.Fail()
					return 0, ctx.Err()

				default:
				}

				return writer.Write(p)
			})); err != nil {
				writer.Fail()

				log.Error(err, "failed to download file", "filename", f.Filename())
				saver.Remove()
				continue
			}

			saver.Close()
			writer.Done()
			log.Info("downloaded document", "filename", f.Filename())
		}
	}
}

// Stop stops the pool of workers and waits for them to finish.
func (p *Downloader) Stop() error {
	defer p.renderer.Stop()

	p.queueWG.Wait()

	close(p.files)
	return p.workerG.Wait()
}

// AddSingleDownload adds a single file to the download queue.
func (p *Downloader) AddDownload(file telegram.File, saveOptions ...SaveFileOption) {
	opts := saveFileOption{}
	for _, opt := range saveOptions {
		opt(&opts)
	}

	p.files <- FileInfo{
		File: file,
		opts: opts,
	}
}

// AddDownloadQueue adds a channel of files to the download queue.
func (p *Downloader) AddDownloadQueue(ctx context.Context, files <-chan telegram.File, saveOptions ...SaveFileOption) {
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

				p.AddDownload(file, saveOptions...)
			}
		}
	}()
}

func (p *Downloader) tryAddFileToSaver(s MultiSaver, dir string, f FileInfo) error {
	filepath := path.Join(dir, f.Filename())

	exists, err := afero.Exists(p.fs, filepath)
	if err != nil {
		return err
	}

	if !exists {
		return s.AddFile(filepath)
	} else if f.opts.saveOnlyIfNew {
		return nil
	}

	fileExt := path.Ext(f.Filename())
	fileWithoutExt := strings.TrimSuffix(f.Filename(), fileExt)

	// File exists, generate a new filename
	i := 1
	for {
		newFilename := fmt.Sprintf("%s_%d%s", fileWithoutExt, i, fileExt)
		newFilepath := path.Join(dir, newFilename)

		exists, err := afero.Exists(p.fs, newFilepath)
		if err != nil {
			return nil
		}

		// File does not exist, return the new filename
		if !exists {
			return s.AddFile(newFilepath)
		}

		i++
	}
}

// createFileSaver creates a file saver for the file
func (p *Downloader) createSaver(f FileInfo) (Saver, error) {
	subdir := strconv.FormatInt(f.PeerID(), 10)
	username, ok := f.Username()
	if ok && username != "" {
		subdir = username
	}

	outputDir := path.Join(p.outputDir, subdir)
	if err := p.createDirectoryIfNotExists(outputDir); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	multiSaver := NewAferoMultiSaver(p.fs)

	if f.opts.saveByHashtags {
		for _, hashtag := range f.Hashtags() {
			hashtagDir := path.Join(outputDir, hashtag)
			if err := p.createDirectoryIfNotExists(hashtagDir); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}

			if err := p.tryAddFileToSaver(multiSaver, hashtagDir, f); err != nil {
				return nil, err
			}
		}

		if multiSaver.IsValid() {
			return multiSaver, nil
		}
	}

	if err := p.tryAddFileToSaver(multiSaver, outputDir, f); err != nil {
		return nil, err
	}

	return multiSaver, nil
}

// createDirectoryIfNotExists creates a directory and all parent directories if it does not exist
func (p *Downloader) createDirectoryIfNotExists(dir string) error {
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
