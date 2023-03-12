package dwpool

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/ctxlogger"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// Pool is a pool of workers that download files
type Downloader struct {
	outputDir string
	fs        afero.Fs
	files     chan fileInfo
	renderer  *renderer.DownloadRenderer
	service   telegram.FileService
	g         *errgroup.Group
	workers   int
}

// NewDownloader creates a new pool of workers.
func NewDownloader(fs afero.Fs, service telegram.FileService, workers int) *Downloader {
	return &Downloader{
		fs:       fs,
		files:    make(chan fileInfo),
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
	logger := ctxlogger.FromContext(ctx)
	logger.Info("Downloader started", zap.Int("workers", d.workers))

	d.g, ctx = errgroup.WithContext(ctx)
	for i := 0; i < d.workers; i++ {
		d.g.Go(func() error {
			return d.worker(ctx)
		})
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

// worker is a worker that downloads files.
func (d *Downloader) worker(ctx context.Context) error {
	logger := ctxlogger.FromContext(ctx)

	defer logger.Debug("worker stopped")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case f, ok := <-d.files:
			if !ok {
				logger.Debug("no more jobs")
				return nil
			}

			logger.Debug("found job", zap.Stringer("file", f.f))
			file, err := d.createFile(ctx, fileInfo{
				f: f.f,
			})
			if err != nil {
				logger.Error("failed to create file", zap.Error(err))
				continue
			}

			writer := d.renderer.TrackedWriter(f.f.Filename(), f.f.Size(), file)
			if err := d.service.Download(ctx, f.f, writerFunc(func(p []byte) (int, error) {
				select {
				case <-ctx.Done():
					writer.Fail()
					return 0, ctx.Err()

				default:
				}

				return writer.Write(p)
			})); err != nil {
				writer.Fail()

				logger.Error("failed to download document", zap.Error(err))
				file.Close()
				file.Remove()
				continue
			}

			file.Close()
			writer.Done()
			logger.Debug("downloaded document", zap.String("filename", f.f.Filename()))
		}
	}
}

// Stop stops the pool of workers and waits for them to finish.
func (p *Downloader) Stop(ctx context.Context) error {
	defer func() {
		logger := ctxlogger.FromContext(ctx)
		logger.Info("Downloader stopped")
		p.renderer.Stop()
	}()

	close(p.files)
	return p.g.Wait()
}

// DownloadFile adds a single file to the download queue.
func (p *Downloader) DownloadFile(file telegram.FileInfo) {
	p.files <- fileInfo{
		f: file,
	}
}

func (p *Downloader) RunAsyncDownloader(ctx context.Context, files <-chan telegram.FileInfo) {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()

			case file, ok := <-files:
				if !ok {
					return nil
				}

				p.files <- fileInfo{
					f: file,
				}
			}
		}
	})

	g.Wait()
}

// createFile creates a file with a unique filename in the output directory
func (p *Downloader) createFile(ctx context.Context, info fileInfo) (*file, error) {
	outputDir := path.Join(p.outputDir, info.Subdir())
	if err := p.createDirectoryIfNotExists(outputDir); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	filename, err := p.getUniqueFilename(outputDir, info.Filename())
	if err != nil {
		return nil, err
	}

	f, err := p.fs.Create(path.Join(outputDir, filename))
	if err != nil {
		return nil, err
	}

	return &file{
		fs:   p.fs,
		file: f,
	}, nil
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

// getUniqueFilename returns a unique filename by appending a number to the end of the filename if it already exists
func (p *Downloader) getUniqueFilename(dir string, filename string) (string, error) {
	fullPath := path.Join(dir, filename)
	fileExt := path.Ext(filename)
	fileNameOnly := strings.TrimSuffix(filename, fileExt)

	ok, err := afero.Exists(p.fs, fullPath)
	if err != nil {
		return "", err
	}

	// File exists, generate a new filename
	if ok {
		i := 1
		for {
			newFilename := fmt.Sprintf("%s_%d%s", fileNameOnly, i, fileExt)
			newFullPath := path.Join(dir, newFilename)

			ok, err := afero.Exists(p.fs, newFullPath)
			if err != nil {
				return "", err
			}

			// File does not exist, return the new filename
			if !ok {
				return newFilename, nil
			}

			i++
		}
	}

	return filename, nil
}
