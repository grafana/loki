package cassandra

import (
	"context"
	"fmt"
	"sync"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

type tableClient struct {
	cfg           Config
	session       *gocql.Session
	mtx           sync.Mutex
	clusterConfig *gocql.ClusterConfig
}

// NewTableClient returns a new TableClient.
func NewTableClient(_ context.Context, cfg Config, registerer prometheus.Registerer) (index.TableClient, error) {
	session, clusterConfig, err := cfg.session("table-manager", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &tableClient{
		cfg:           cfg,
		session:       session,
		clusterConfig: clusterConfig,
	}, nil
}

func (c *tableClient) reconnectTableSession() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	newSession, err := c.clusterConfig.CreateSession()
	if err != nil {
		return err
	}
	c.session.Close()
	c.session = newSession
	return nil
}

func (c *tableClient) ListTables(_ context.Context) ([]string, error) {
	result, err := c.listTables()
	//
	if errors.Cause(err) == gocql.ErrNoConnections {
		connectErr := c.reconnectTableSession()
		if connectErr != nil {
			return nil, errors.Wrap(err, "tableClient ListTables reconnect fail")
		}
		// retry after reconnect
		result, err = c.listTables()
	}
	return result, err
}

func (c *tableClient) listTables() ([]string, error) {
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

func (c *tableClient) CreateTable(ctx context.Context, desc config.TableDesc) error {
	err := c.createTable(ctx, desc)
	//
	if errors.Cause(err) == gocql.ErrNoConnections {
		connectErr := c.reconnectTableSession()
		if connectErr != nil {
			return errors.Wrap(err, "tableClient CreateTable reconnect fail")
		}
		// retry after reconnect
		err = c.createTable(ctx, desc)
	}
	return err
}

func (c *tableClient) createTable(ctx context.Context, desc config.TableDesc) error {
	query := c.getCreateTableQuery(&desc)
	err := c.session.Query(query).WithContext(ctx).Exec()
	return errors.WithStack(err)
}

func (c *tableClient) DeleteTable(ctx context.Context, name string) error {
	err := c.deleteTable(ctx, name)
	//
	if errors.Cause(err) == gocql.ErrNoConnections {
		connectErr := c.reconnectTableSession()
		if connectErr != nil {
			return errors.Wrap(err, "tableClient DeleteTable reconnect fail")
		}
		// retry after reconnect
		err = c.deleteTable(ctx, name)
	}
	return err
}

func (c *tableClient) deleteTable(ctx context.Context, name string) error {
	err := c.session.Query(fmt.Sprintf(`
		DROP TABLE IF EXISTS %s;`, name)).WithContext(ctx).Exec()
	return errors.WithStack(err)
}

func (c *tableClient) DescribeTable(_ context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	return config.TableDesc{
		Name: name,
	}, true, nil
}

func (c *tableClient) UpdateTable(_ context.Context, _, _ config.TableDesc) error {
	return nil
}

func (c *tableClient) Stop() {
	c.session.Close()
}

func (c *tableClient) getCreateTableQuery(desc *config.TableDesc) (query string) {
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
