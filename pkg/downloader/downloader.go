package downloader

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/dropbox"
	"github.com/spf13/afero"
	"go.uber.org/zap"
)

type Downloader interface {
	Create(ctx context.Context, file FileInfo) (File, error)
}

type downloader struct {
	outputDir string
	log       *zap.Logger
	fs        afero.Fs
}

var _ Downloader = (*downloader)(nil)

func getFS(ctx context.Context, cfg config.Config, log *zap.Logger) (afero.Fs, error) {
	switch strings.ToLower(cfg.GetString("type")) {
	case "local":
		return afero.NewOsFs(), nil

	case "dropbox":
		return dropbox.NewFs(ctx, cfg.Sub("dropbox"), log.Named("dropbox"))

	default:
		return nil, fmt.Errorf("unknown file system type: %s", cfg.GetString("type"))
	}
}

func NewDownloader(ctx context.Context, cfg config.Config, log *zap.Logger) (Downloader, error) {
	fs, err := getFS(ctx, cfg, log)
	if err != nil {
		return nil, err
	}

	return &downloader{
		outputDir: cfg.GetString("dir.output"),
		log:       log,
		fs:        fs,
	}, nil
}

func (d *downloader) Create(ctx context.Context, info FileInfo) (File, error) {
	d.log.Info("creating file", zap.String("filename", info.Filename()), zap.String("subdir", info.Subdir()))

	outputDir := path.Join(d.outputDir, info.Subdir())
	d.log.Info("output directory", zap.String("outputDir", outputDir))
	if err := createDirectoryIfNotExists(d.fs, outputDir); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	filename, err := getUniqueFilename(d.fs, outputDir, info.Filename())
	if err != nil {
		return nil, err
	}

	f, err := d.fs.Create(path.Join(outputDir, filename))
	if err != nil {
		return nil, err
	}

	return &file{
		fs:   d.fs,
		file: f,
	}, nil
}

// createDirectoryIfNotExists creates a directory and all parent directories if it does not exist
func createDirectoryIfNotExists(fs afero.Fs, dir string) error {
	ok, err := afero.DirExists(fs, dir)
	if err != nil {
		return err
	}

	if !ok {
		err = fs.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}

// getUniqueFilename returns a unique filename by appending a number to the end of the filename if it already exists
func getUniqueFilename(fs afero.Fs, dir string, filename string) (string, error) {
	fullPath := path.Join(dir, filename)
	fileExt := path.Ext(filename)
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
			newFullPath := path.Join(dir, newFilename)

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
