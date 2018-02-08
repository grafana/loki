package cassandra

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/gocql/gocql"

	"github.com/weaveworks/cortex/pkg/chunk"
)

type tableClient struct {
	cfg     Config
	session *gocql.Session
}

// NewTableClient returns a new TableClient.
func NewTableClient(ctx context.Context, cfg Config) (chunk.TableClient, error) {
	session, err := cfg.session()
	if err != nil {
		return nil, err
	}
	return &tableClient{
		cfg:     cfg,
		session: session,
	}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	md, err := c.session.KeyspaceMetadata(c.cfg.keyspace)
	if err != nil {
		return nil, err
	}
	result := []string{}
	for name := range md.Tables {
		result = append(result, name)
	}
	return result, nil
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	return c.session.Query(fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			hash text,
			range blob,
			value blob,
			PRIMARY KEY (hash, range)
		)`, desc.Name)).WithContext(ctx).Exec()
}

func (c *tableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, status string, err error) {
	return chunk.TableDesc{
		Name: name,
	}, dynamodb.TableStatusActive, nil
}

func (c *tableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	return nil
}
