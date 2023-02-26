package downloader

import (
	"io"
	"path/filepath"

	"github.com/spf13/afero"
)

type FileInfo interface {
	Filename() string
	Subdir() string
}

type File interface {
	io.Writer

	Abort() error
	Commit() error
}

type file struct {
	d       *downloader
	subdir  string
	tmpFile afero.File
}

var _ File = (*file)(nil)

func (f *file) Write(p []byte) (n int, err error) {
	return f.tmpFile.Write(p)
}

func (f *file) Abort() error {
	f.tmpFile.Close()

	return f.d.fs.Remove(f.tmpFile.Name())
}

func (f *file) Commit() error {
	f.tmpFile.Close()

	output := filepath.Join(f.d.outputDir, f.subdir)
	if err := f.d.createDirectoryIfNotExists(output); err != nil {
		return err
	}

	newFilename, err := getUniqueFilename(f.d.fs, output, filepath.Base(f.tmpFile.Name()))
	if err != nil {
		return err
	}

	newFile := filepath.Join(output, newFilename)
	return f.d.fs.Rename(f.tmpFile.Name(), newFile)
}
