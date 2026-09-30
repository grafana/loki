# pkg/logline/internal/v5/

v5 is **in development**. Do not set `-logline-index.version=v5`.

This package is a fork of `internal/v3` (on-disk format, footer version 4) with
`ngrams.go` copied from `internal/v4`. Nothing about the format or the
extractor has changed yet. Later changes belong here, not in v3 or v4.

`CurrentVersion` stays `"v3"`. Footer auto-detection cannot tell a v5 file
from a v3 or v4 file and reports `"v3"`. The query path takes the version
from `meta.json`.
