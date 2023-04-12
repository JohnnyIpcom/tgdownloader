package dwpool

import (
	"strconv"

	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
)

type FileInfo struct {
	telegram.FileInfo
}

func (f *FileInfo) Subdir() string {
	username, ok := f.Username()
	if ok && username != "" {
		return username
	}

	return strconv.FormatInt(f.PeerID(), 10)
}
