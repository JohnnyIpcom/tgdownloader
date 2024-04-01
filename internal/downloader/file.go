package downloader

import (
	"io"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/afero"
)

type File struct {
	telegram.File

	subdirs        []string
	saveByHashtags bool
}

type FileOption func(*File)

func WithSubdirs(subdirs ...string) FileOption {
	return func(o *File) {
		// Ensure subdirs are unique
		subdirsMap := make(map[string]struct{})
		for _, subdir := range subdirs {
			subdirsMap[subdir] = struct{}{}
		}

		// create subdirs slice if it does not exist and append subdirs to it
		if o.subdirs == nil {
			o.subdirs = make([]string, 0, len(subdirsMap))
		}
		for subdir := range subdirsMap {
			o.subdirs = append(o.subdirs, subdir)
		}
	}
}

func WithSaveByHashtags(saveByHashtags bool) FileOption {
	return func(o *File) {
		o.saveByHashtags = saveByHashtags
	}
}

func NewFile(file telegram.File, opts ...FileOption) File {
	f := File{
		File: file,
	}

	for _, opt := range opts {
		opt(&f)
	}

	metadata := file.Metadata()
	if metadata != nil {
		if peername, ok := metadata["peername"]; ok {
			f.subdirs = append(f.subdirs, peername.(string))
		}

		if f.saveByHashtags {
			if hashtags, ok := metadata["hashtags"]; ok {
				f.subdirs = append(f.subdirs, hashtags.([]string)...)
			}
		}
	}

	return f
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
// aferoSaver is an implementation of MultiSaver that uses afero.Fs
// to create and write to files
//

type aferoSaver struct {
	fs    afero.Fs
	files []afero.File
}

var _ MultiSaver = &aferoSaver{}

func NewAferoSaver(fs afero.Fs) MultiSaver {
	return &aferoSaver{
		fs:    fs,
		files: make([]afero.File, 0),
	}
}

func (m *aferoSaver) AddFile(filename string) error {
	file, err := m.fs.Create(filename)
	if err != nil {
		return err
	}

	m.files = append(m.files, file)
	return nil
}

func (m *aferoSaver) IsValid() bool {
	return len(m.files) > 0
}

func (m *aferoSaver) Write(p []byte) (n int, err error) {
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

func (m *aferoSaver) Close() error {
	var err error
	for _, file := range m.files {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

func (m *aferoSaver) Remove() error {
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

var _ MultiSaver = &nullSaver{}

func NewNullSaver() MultiSaver {
	return &nullSaver{}
}

func (s *nullSaver) AddFile(filename string) error {
	return nil
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
