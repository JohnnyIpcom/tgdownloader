package downloader

import (
	"io"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
)

type FileInfo struct {
	telegram.File

	opts saveFileOption
}

type Saver interface {
	io.WriteCloser

	// IsValid returns true if there are files to write to
	IsValid() bool

	// Remove removes all files created by this MultiSaver
	Remove() error
}

// MultiSaver is an io.WriteCloser that writes to multiple files
type MultiSaver interface {
	Saver

	// AddFile creates a new file with the given filename in the MultiSaver
	AddFile(filename string) error
}

//
// AferoMultiSaver is an implementation of MultiSaver that uses afero.Fs
// to create and write to files
//

type aferoMultiSaver struct {
	fs    afero.Fs
	files []afero.File
}

var _ MultiSaver = &aferoMultiSaver{}

func NewAferoMultiSaver(fs afero.Fs) MultiSaver {
	return &aferoMultiSaver{
		fs:    fs,
		files: make([]afero.File, 0),
	}
}

func (m *aferoMultiSaver) AddFile(filename string) error {
	file, err := m.fs.Create(filename)
	if err != nil {
		return err
	}

	m.files = append(m.files, file)
	return nil
}

func (m *aferoMultiSaver) IsValid() bool {
	return len(m.files) > 0
}

func (m *aferoMultiSaver) Write(p []byte) (n int, err error) {
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

func (m *aferoMultiSaver) Close() error {
	var err error
	for _, file := range m.files {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

func (m *aferoMultiSaver) Remove() error {
	if err := m.Close(); err != nil {
		return err
	}

	var err error
	for _, file := range m.files {
		if cerr := m.fs.Remove(file.Name()); cerr != nil && err == nil {
			err = cerr
		}
	}

	return err
}

//
// NullSaver is an implementation of Saver that does nothing
//

type nullSaver struct{}

var _ Saver = &nullSaver{}

func NewNullSaver() Saver {
	return &nullSaver{}
}

func (s *nullSaver) Write(p []byte) (n int, err error) {
	return len(p), nil
}

func (s *nullSaver) Close() error {
	return nil
}

func (s *nullSaver) IsValid() bool {
	return true
}

func (s *nullSaver) Remove() error {
	return nil
}
