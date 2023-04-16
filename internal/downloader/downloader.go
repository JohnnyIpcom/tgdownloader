package downloader

import (
	"context"
	"fmt"
	"path"
	"runtime"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/johnnyipcom/tgdownloader/internal/renderer"
	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

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
func NewDownloader(cfg config.Config, log *zap.Logger, service telegram.FileService) *Downloader {
	workers := cfg.GetInt("threads")
	if workers < 1 {
		workers = runtime.NumCPU()
	}

	return &Downloader{
		fs:       getDownloaderFS(cfg, log),
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
			file, err := d.createFile(ctx, f)
			if err != nil {
				log.Info("failed to create file", zap.Error(err))
				continue
			}

			writer := d.renderer.TrackedWriter(f.Filename(), f.Size(), file)
			if err := d.service.Download(ctx, f.FileInfo, writerFunc(func(p []byte) (int, error) {
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
				file.Close()
				d.fs.Remove(file.Name())
				continue
			}

			file.Close()
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
func (p *Downloader) AddSingleDownload(file telegram.FileInfo) {
	p.files <- FileInfo{FileInfo: file}
}

// AddDownloadQueue adds a channel of files to the download queue.
func (p *Downloader) AddDownloadQueue(ctx context.Context, files <-chan telegram.FileInfo) {
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

				p.AddSingleDownload(file)
			}
		}
	}()
}

// createFile creates a file with a unique filename in the output directory
func (p *Downloader) createFile(ctx context.Context, f FileInfo) (afero.File, error) {
	outputDir := path.Join(p.outputDir, f.Subdir())
	if err := p.createDirectoryIfNotExists(outputDir); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	filename, err := p.getUniqueFilename(outputDir, f.Filename())
	if err != nil {
		return nil, err
	}

	return p.fs.Create(path.Join(outputDir, filename))
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
