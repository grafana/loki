package avatica

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/pkg/errors"
)

type tableClient struct {
	cfg     Config
	session *sql.DB
}

// NewTableClient returns a new TableClient.
func NewTableClient(ctx context.Context, cfg Config) (chunk.TableClient, error) {
	session, err := cfg.session()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &tableClient{
		cfg:     cfg,
		session: session,
	}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	rows, err := c.session.Query("SHOW TABLES")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	result := []string{}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	tableVals := make([]interface{}, len(columns))
	for idx, _ := range tableVals {
		var val interface{}
		tableVals[idx] = &val
	}
	for rows.Next() {
		err := rows.Scan(tableVals...)
		if err != nil {
			return nil, err
		}
		valPoint := tableVals[0].(*interface{})
		val := *valPoint
		tableName := val.(string)
		result = append(result, tableName)
	}
	return result, nil
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			hash VARCHAR,
			range VARBINARY,
			value VARBINARY,
			PRIMARY KEY (hash, range)
		)`, desc.Name)
	if c.cfg.TableOptions != "" {
		query = fmt.Sprintf("%s WITH %s", query, c.cfg.TableOptions)
	}
	_, err := c.session.Query(query)
	return errors.WithStack(err)
}

func (c *tableClient) DeleteTable(ctx context.Context, name string) error {
	if c.cfg.Backend == BACKEND_ALIBABACLOUD_LINDORM {
		_, err := c.session.Query(fmt.Sprintf(`
		OFFLINE TABLE %s`, name))
		if err != nil {
			return errors.WithStack(err)
		}
	}
	_, err := c.session.Query(fmt.Sprintf(`
		DROP TABLE IF EXISTS %s`, name))
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
