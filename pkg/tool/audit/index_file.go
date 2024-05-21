package audit

import (
	"github.com/go-kit/log"
)

type indexFile struct {
	path   string
	logger log.Logger
}

func newIndexFile(logger log.Logger) *indexFile {
	return &indexFile{
		logger: logger,
	}
}

// func (i *indexFile) getSourceFile(ctx context.Context, prefix, key string) error {
// 	// TODO(chaudum): make work directory configurable
// 	filename := path.Base(key)

// 	if err := os.MkdirAll(filepath.Join(workingDir, prefix), 0750); err != nil {
// 		return err
// 	}

// 	isCompressed := shipperutil.IsCompressedFile(filename)
// 	dst := filepath.Join(workingDir, prefix, filename)
// 	if isCompressed {
// 		dst = strings.Trim(dst, ".gz")
// 	}

// 	err := shipperutil.DownloadFileFromStorage(dst, isCompressed, false, i.logger, func() (io.ReadCloser, error) {
// 		r, _, err := i.client.GetObject(ctx, i.object.Key)
// 		return r, err
// 	})
// 	if err != nil {
// 		return err
// 	}
// 	i.path = dst
// 	return nil
// }

// func usage() error {
// 	idx, err := newIndexFile("something.gz", client, logger)
// 	if err != nil {
// 		level.Warn(s.logger).Log("msg", "failed to load index", "table", table, "error", err)
// 		return keys, fmt.Errorf("failed to load index: %w", err)
// 	}
// 	if idx == nil {
// 		level.Warn(s.logger).Log("msg", "no files found in table", "table", table)
// 		continue
// 	}

// 	if err := idx.getSourceFile(ctx, s.tenant); err != nil {
// 		return noFiles, err
// 	}
// }
