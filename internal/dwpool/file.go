package dwpool

import (
	"strconv"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
)

type fileInfo struct {
	f telegram.FileInfo
}

func (f *fileInfo) Filename() string {
	return f.f.Filename()
}

func (f *fileInfo) Subdir() string {
	if f.f.Username() != "" {
		return f.f.Username()
	}

	return strconv.FormatInt(f.f.FromID(), 10)
}

type file struct {
	fs   afero.Fs
	file afero.File
}

func (f *file) Write(p []byte) (n int, err error) {
	return f.file.Write(p)
}

func (f *file) Close() error {
	return f.file.Close()
}

func (f *file) Remove() error {
	return f.fs.Remove(f.file.Name())
}
