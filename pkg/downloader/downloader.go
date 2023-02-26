package downloader

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type Downloader interface {
	Prepare() error
	Cleanup()

	Create(ctx context.Context, file FileInfo) (File, error)
}

type downloader struct {
	outputDir string
	tmpDir    string
	log       *zap.Logger
	fs        afero.Fs
}

var _ Downloader = (*downloader)(nil)

func NewDownloader(cfg config.Config, log *zap.Logger) (Downloader, error) {
	return &downloader{
		outputDir: cfg.GetString("dir.output"),
		tmpDir:    cfg.GetString("dir.temp"),
		log:       log,
		fs:        afero.NewOsFs(),
	}, nil
}

func (d *downloader) Prepare() error {
	d.log.Info("creating output directory", zap.String("dir", d.outputDir))
	if err := d.createDirectoryIfNotExists(d.outputDir); err != nil {
		return err
	}

	d.log.Info("creating tmp directory", zap.String("dir", d.tmpDir))
	if err := d.createDirectoryIfNotExists(d.tmpDir); err != nil {
		return err
	}

	return nil
}

func (d *downloader) Create(ctx context.Context, info FileInfo) (File, error) {
	d.log.Info("creating file", zap.String("filename", info.Filename()), zap.String("subdir", info.Subdir()))

	filename, err := getUniqueFilename(d.fs, d.tmpDir, info.Filename())
	if err != nil {
		return nil, err
	}

	f, err := d.fs.Create(filepath.Clean(filepath.Join(d.tmpDir, filename)))
	if err != nil {
		return nil, err
	}

	return &file{
		d:       d,
		subdir:  info.Subdir(),
		tmpFile: f,
	}, nil
}

func (d *downloader) Cleanup() {
	d.log.Info("removing tmp directory", zap.String("dir", d.tmpDir))
	if err := d.removeDirectoryIfExists(d.tmpDir); err != nil {
		d.log.Error("failed to remove tmp directory", zap.Error(err))
	}
}

func (d *downloader) createDirectoryIfNotExists(dir string) error {
	ok, err := afero.DirExists(d.fs, dir)
	if err != nil {
		return err
	}

	if !ok {
		err = d.fs.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *downloader) removeDirectoryIfExists(dir string) error {
	ok, err := afero.DirExists(d.fs, dir)
	if err != nil {
		return err
	}

	if ok {
		err = d.fs.RemoveAll(dir)
		if err != nil {
			return err
		}
	}

	return nil
}

func getUniqueFilename(fs afero.Fs, path string, filename string) (string, error) {
	fullPath := filepath.Join(path, filename)
	fileExt := filepath.Ext(filename)
	fileNameOnly := strings.TrimSuffix(filename, fileExt)

	ok, err := afero.Exists(fs, fullPath)
	if err != nil {
		return "", err
	}

	// File exists, generate a new filename
	if ok {
		i := 1
		for {
			newFilename := fmt.Sprintf("%s_%d%s", fileNameOnly, i, fileExt)
			newFullPath := filepath.Join(path, newFilename)

			ok, err := afero.Exists(fs, newFullPath)
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
