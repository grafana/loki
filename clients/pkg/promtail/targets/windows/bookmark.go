//+build windows

package windows

import (
	"io/ioutil"
	"os"

	"github.com/spf13/afero"

	"github.com/grafana/loki/clients/pkg/promtail/targets/windows/win_eventlog"
)

type bookMark struct {
	handle win_eventlog.EvtHandle
	file   afero.File
	isNew  bool

	buf []byte
}

// newBookMark creates a new windows event bookmark.
// The bookmark will be saved at the given path. Use save to save the current position for a given event.
func newBookMark(path string) (*bookMark, error) {
	// 16kb buffer for rendering bookmark
	buf := make([]byte, 16<<10)

	_, err := fs.Stat(path)
	// creates a new bookmark file if none exists.
	if os.IsNotExist(err) {
		file, err := fs.Create(path)
		if err != nil {
			return nil, err
		}
		bm, err := win_eventlog.CreateBookmark("")
		if err != nil {
			return nil, err
		}
		return &bookMark{
			handle: bm,
			file:   file,
			isNew:  true,
			buf:    buf,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	// otherwise open the current one.
	file, err := fs.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}
	fileContent, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	fileString := string(fileContent)
	// load the current bookmark.
	bm, err := win_eventlog.CreateBookmark(fileString)
	if err != nil {
		return nil, err
	}
	return &bookMark{
		handle: bm,
		file:   file,
		isNew:  fileString == "",
		buf:    buf,
	}, nil
}

// save Saves the bookmark at the current event position.
func (b *bookMark) save(event win_eventlog.EvtHandle) error {
	newBookmark, err := win_eventlog.UpdateBookmark(b.handle, event, b.buf)
	if err != nil {
		return err
	}
	if err := b.file.Truncate(0); err != nil {
		return err
	}
	if _, err := b.file.Seek(0, 0); err != nil {
		return err
	}
	_, err = b.file.WriteString(newBookmark)
	return err
}

// close closes the current bookmark file.
func (b *bookMark) close() error {
	return b.file.Close()
}
