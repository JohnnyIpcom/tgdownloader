package downloader

import (
	"io"

	"github.com/spf13/afero"
)

type FileInfo interface {
	Filename() string
	Subdir() string
}

type File interface {
	io.WriteCloser

	Remove() error
}

type file struct {
	fs   afero.Fs
	file afero.File
}

var _ File = (*file)(nil)

func (f *file) Write(p []byte) (n int, err error) {
	return f.file.Write(p)
}

func (f *file) Close() error {
	return f.file.Close()
}

func (f *file) Remove() error {
	return f.fs.Remove(f.file.Name())
}
