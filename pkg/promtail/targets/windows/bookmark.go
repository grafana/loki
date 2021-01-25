package windows

import (
	"io/ioutil"
	"os"

	"github.com/grafana/loki/pkg/promtail/targets/windows/win_eventlog"
	"github.com/spf13/afero"
)

type bookMark struct {
	handle win_eventlog.EvtHandle
	file   afero.File
	isNew  bool

	buf []byte
}

func newBookMark(path string) (*bookMark, error) {
	buf := make([]byte, 1<<14)
	_, err := fs.Stat(path)
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
	file, err := fs.OpenFile(path, os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}
	fileContent, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	fileString := string(fileContent)
	bm, err := win_eventlog.CreateBookmark(fileString)
	if err != nil {
		return nil, err
	}
	return &bookMark{
		handle: bm,
		file:   file,
		isNew:  fileString == "",
		// 4kb buffer for rendering bookmark
		buf: buf,
	}, nil
}

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

func (b *bookMark) close() error {
	return b.file.Close()
}
