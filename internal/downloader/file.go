package downloader

import (
	"io"
	"strconv"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
)

type FileInfo struct {
	telegram.File

	saveByHashtags bool
}

func (f *FileInfo) Subdir() string {
	username, ok := f.Username()
	if ok && username != "" {
		return username
	}

	return strconv.FormatInt(f.PeerID(), 10)
}

func (f *FileInfo) Hashtags() []string {
	if !f.saveByHashtags {
		return nil
	}

	return f.File.Hashtags()
}

type MultiFile struct {
	files []afero.File
}

func NewMultiFile(fs afero.Fs, filenames []string) (*MultiFile, error) {
	files := make([]afero.File, len(filenames))
	for i, filename := range filenames {
		file, err := fs.Create(filename)
		if err != nil {
			return nil, err
		}

		files[i] = file
	}

	return &MultiFile{files: files}, nil
}

func (m *MultiFile) Write(p []byte) (n int, err error) {
	for _, file := range m.files {
		written, err := file.Write(p)
		if err != nil {
			return n, err
		}
		if written != len(p) {
			return n, io.ErrShortWrite
		}
		n += written
	}

	return n, nil
}

func (m *MultiFile) Close() error {
	var err error
	for _, file := range m.files {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

func (m *MultiFile) Remove(fs afero.Fs) error {
	if err := m.Close(); err != nil {
		return err
	}

	var err error
	for _, file := range m.files {
		if cerr := fs.Remove(file.Name()); cerr != nil && err == nil {
			err = cerr
		}
	}

	return err
}
