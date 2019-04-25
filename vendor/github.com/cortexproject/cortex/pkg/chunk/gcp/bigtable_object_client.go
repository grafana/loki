package gcp

import (
	"context"
	"fmt"

	"cloud.google.com/go/bigtable"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
)

type bigtableObjectClient struct {
	cfg       Config
	schemaCfg chunk.SchemaConfig
	client    *bigtable.Client
}

// NewBigtableObjectClient makes a new chunk.ObjectClient that stores chunks in
// Bigtable.
func NewBigtableObjectClient(ctx context.Context, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.ObjectClient, error) {
	opts := toOptions(cfg.GRPCClientConfig.DialOption(bigtableInstrumentation()))
	client, err := bigtable.NewClient(ctx, cfg.Project, cfg.Instance, opts...)
	if err != nil {
		return nil, err
	}
	return newBigtableObjectClient(cfg, schemaCfg, client), nil
}

func newBigtableObjectClient(cfg Config, schemaCfg chunk.SchemaConfig, client *bigtable.Client) chunk.ObjectClient {
	return &bigtableObjectClient{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		client:    client,
	}
}

func (s *bigtableObjectClient) Stop() {
	s.client.Close()
}

func (s *bigtableObjectClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	keys := map[string][]string{}
	muts := map[string][]*bigtable.Mutation{}

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}
		key := chunks[i].ExternalKey()
		tableName, err := s.schemaCfg.ChunkTableFor(chunks[i].From)
		if err != nil {
			return err
		}
		keys[tableName] = append(keys[tableName], key)

		mut := bigtable.NewMutation()
		mut.Set(columnFamily, column, 0, buf)
		muts[tableName] = append(muts[tableName], mut)
	}

	for tableName := range keys {
		table := s.client.Open(tableName)
		errs, err := table.ApplyBulk(ctx, keys[tableName], muts[tableName])
		if err != nil {
			return err
		}
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *bigtableObjectClient) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	sp, ctx := ot.StartSpanFromContext(ctx, "GetChunks")
	defer sp.Finish()
	sp.LogFields(otlog.Int("chunks requested", len(input)))

	chunks := map[string]map[string]chunk.Chunk{}
	keys := map[string]bigtable.RowList{}
	for _, c := range input {
		tableName, err := s.schemaCfg.ChunkTableFor(c.From)
		if err != nil {
			return nil, err
		}
		key := c.ExternalKey()
		keys[tableName] = append(keys[tableName], key)
		if _, ok := chunks[tableName]; !ok {
			chunks[tableName] = map[string]chunk.Chunk{}
		}
		chunks[tableName][key] = c
	}

	outs := make(chan chunk.Chunk, len(input))
	errs := make(chan error, len(input))

	for tableName := range keys {
		var (
			table  = s.client.Open(tableName)
			keys   = keys[tableName]
			chunks = chunks[tableName]
		)

		for i := 0; i < len(keys); i += maxRowReads {
			page := keys[i:util.Min(i+maxRowReads, len(keys))]
			go func(page bigtable.RowList) {
				decodeContext := chunk.NewDecodeContext()

				var processingErr error
				var receivedChunks = 0

				// rows are returned in key order, not order in row list
				err := table.ReadRows(ctx, page, func(row bigtable.Row) bool {
					chunk, ok := chunks[row.Key()]
					if !ok {
						processingErr = errors.WithStack(fmt.Errorf("Got row for unknown chunk: %s", row.Key()))
						return false
					}

					err := chunk.Decode(decodeContext, row[columnFamily][0].Value)
					if err != nil {
						processingErr = err
						return false
					}

					receivedChunks++
					outs <- chunk
					return true
				})

				if processingErr != nil {
					errs <- processingErr
				} else if err != nil {
					errs <- errors.WithStack(err)
				} else if receivedChunks < len(page) {
					errs <- errors.WithStack(fmt.Errorf("Asked for %d chunks for Bigtable, received %d", len(page), receivedChunks))
				}
			}(page)
		}
	}

	output := make([]chunk.Chunk, 0, len(input))
	for i := 0; i < len(input); i++ {
		select {
		case c := <-outs:
			output = append(output, c)
		case err := <-errs:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return output, nil
}
