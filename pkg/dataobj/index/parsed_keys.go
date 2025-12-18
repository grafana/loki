package index

import (
	"context"
	"slices"
	"unsafe"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
)

type parsedKeysSet struct {
	parsedKeys   map[string]struct{}
	logfmtParser *logfmt.Decoder
}

func (c *parsedKeysSet) Prepare(_ context.Context, _ *dataobj.Section, _ logs.Stats) error {
	c.parsedKeys = make(map[string]struct{}, 100)
	c.logfmtParser = logfmt.NewDecoder(nil)
	return nil
}

func (c *parsedKeysSet) ProcessBatch(_ context.Context, context *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		c.logfmtParser.Reset(log.Line)
		for c.logfmtParser.ScanKeyval() {
			if len(c.logfmtParser.Value()) == 0 {
				continue
			}
			if _, ok := c.parsedKeys[unsafeString(c.logfmtParser.Key())]; ok {
				continue
			}
			c.parsedKeys[string(c.logfmtParser.Key())] = struct{}{}
		}
	}
	return nil
}

func (c *parsedKeysSet) Flush(_ context.Context, logsCtx *logsCalculationContext) error {
	keys := make([]string, 0, len(c.parsedKeys))
	for key := range c.parsedKeys {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return logsCtx.builder.RecordParsedKeys(logsCtx.tenantID, logsCtx.objectPath, logsCtx.sectionIdx, keys)
}

func unsafeString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
