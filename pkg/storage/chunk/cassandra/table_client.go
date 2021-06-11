package cassandra

import (
	"context"
	"fmt"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/chunk"
)

type tableClient struct {
	cfg     Config
	session *gocql.Session
}

// NewTableClient returns a new TableClient.
func NewTableClient(ctx context.Context, cfg Config, registerer prometheus.Registerer) (chunk.TableClient, error) {
	session, err := cfg.session("table-manager", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &tableClient{
		cfg:     cfg,
		session: session,
	}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	md, err := c.session.KeyspaceMetadata(c.cfg.Keyspace)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	result := []string{}
	for name := range md.Tables {
		result = append(result, name)
	}
	return result, nil
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	query := c.getCreateTableQuery(&desc)
	err := c.session.Query(query).WithContext(ctx).Exec()
	return errors.WithStack(err)
}

func (c *tableClient) DeleteTable(ctx context.Context, name string) error {
	err := c.session.Query(fmt.Sprintf(`
		DROP TABLE IF EXISTS %s;`, name)).WithContext(ctx).Exec()
	return errors.WithStack(err)
}

func (c *tableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, isActive bool, err error) {
	return chunk.TableDesc{
		Name: name,
	}, true, nil
}

func (c *tableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	return nil
}

func (c *tableClient) Stop() {
	c.session.Close()
}

func (c *tableClient) getCreateTableQuery(desc *chunk.TableDesc) (query string) {
	query = fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			hash text,
			range blob,
			value blob,
			PRIMARY KEY (hash, range)
		)`, desc.Name)
	if c.cfg.TableOptions != "" {
		query = fmt.Sprintf("%s WITH %s", query, c.cfg.TableOptions)
	}
	return
}
