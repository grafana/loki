package gocql

import (
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	flagCluster      = flag.String("cluster", "127.0.0.1", "a comma-separated list of host:port tuples")
	flagProto        = flag.Int("proto", 0, "protcol version")
	flagCQL          = flag.String("cql", "3.0.0", "CQL version")
	flagRF           = flag.Int("rf", 1, "replication factor for test keyspace")
	clusterSize      = flag.Int("clusterSize", 1, "the expected size of the cluster")
	flagRetry        = flag.Int("retries", 5, "number of times to retry queries")
	flagAutoWait     = flag.Duration("autowait", 1000*time.Millisecond, "time to wait for autodiscovery to fill the hosts poll")
	flagRunSslTest   = flag.Bool("runssl", false, "Set to true to run ssl test")
	flagRunAuthTest  = flag.Bool("runauth", false, "Set to true to run authentication test")
	flagCompressTest = flag.String("compressor", "", "compressor to use")
	flagTimeout      = flag.Duration("gocql.timeout", 5*time.Second, "sets the connection `timeout` for all operations")

	flagCassVersion cassVersion
	clusterHosts    []string
)

func init() {
	flag.Var(&flagCassVersion, "gocql.cversion", "the cassandra version being tested against")

	flag.Parse()
	clusterHosts = strings.Split(*flagCluster, ",")
	log.SetFlags(log.Lshortfile | log.LstdFlags)
}

func addSslOptions(cluster *ClusterConfig) *ClusterConfig {
	if *flagRunSslTest {
		cluster.SslOpts = &SslOptions{
			CertPath:               "testdata/pki/gocql.crt",
			KeyPath:                "testdata/pki/gocql.key",
			CaPath:                 "testdata/pki/ca.crt",
			EnableHostVerification: false,
		}
	}
	return cluster
}

var initOnce sync.Once

func createTable(s *Session, table string) error {
	// lets just be really sure
	if err := s.control.awaitSchemaAgreement(); err != nil {
		log.Printf("error waiting for schema agreement pre create table=%q err=%v\n", table, err)
		return err
	}

	if err := s.Query(table).RetryPolicy(nil).Exec(); err != nil {
		log.Printf("error creating table table=%q err=%v\n", table, err)
		return err
	}

	if err := s.control.awaitSchemaAgreement(); err != nil {
		log.Printf("error waiting for schema agreement post create table=%q err=%v\n", table, err)
		return err
	}

	return nil
}

func createCluster(opts ...func(*ClusterConfig)) *ClusterConfig {
	cluster := NewCluster(clusterHosts...)
	cluster.ProtoVersion = *flagProto
	cluster.CQLVersion = *flagCQL
	cluster.Timeout = *flagTimeout
	cluster.Consistency = Quorum
	cluster.MaxWaitSchemaAgreement = 2 * time.Minute // travis might be slow
	if *flagRetry > 0 {
		cluster.RetryPolicy = &SimpleRetryPolicy{NumRetries: *flagRetry}
	}

	switch *flagCompressTest {
	case "snappy":
		cluster.Compressor = &SnappyCompressor{}
	case "":
	default:
		panic("invalid compressor: " + *flagCompressTest)
	}

	cluster = addSslOptions(cluster)

	for _, opt := range opts {
		opt(cluster)
	}

	return cluster
}

func createKeyspace(tb testing.TB, cluster *ClusterConfig, keyspace string) {
	// TODO: tb.Helper()
	c := *cluster
	c.Keyspace = "system"
	c.Timeout = 30 * time.Second
	session, err := c.CreateSession()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	err = createTable(session, `DROP KEYSPACE IF EXISTS `+keyspace)
	if err != nil {
		panic(fmt.Sprintf("unable to drop keyspace: %v", err))
	}

	err = createTable(session, fmt.Sprintf(`CREATE KEYSPACE %s
	WITH replication = {
		'class' : 'SimpleStrategy',
		'replication_factor' : %d
	}`, keyspace, *flagRF))

	if err != nil {
		panic(fmt.Sprintf("unable to create keyspace: %v", err))
	}
}

func createSessionFromCluster(cluster *ClusterConfig, tb testing.TB) *Session {
	// Drop and re-create the keyspace once. Different tests should use their own
	// individual tables, but can assume that the table does not exist before.
	initOnce.Do(func() {
		createKeyspace(tb, cluster, "gocql_test")
	})

	cluster.Keyspace = "gocql_test"
	session, err := cluster.CreateSession()
	if err != nil {
		tb.Fatal("createSession:", err)
	}

	if err := session.control.awaitSchemaAgreement(); err != nil {
		tb.Fatal(err)
	}

	return session
}

func createSession(tb testing.TB, opts ...func(config *ClusterConfig)) *Session {
	cluster := createCluster(opts...)
	return createSessionFromCluster(cluster, tb)
}

// createTestSession is hopefully moderately useful in actual unit tests
func createTestSession() *Session {
	config := NewCluster()
	config.NumConns = 1
	config.Timeout = 0
	config.DisableInitialHostLookup = true
	config.IgnorePeerAddr = true
	config.PoolConfig.HostSelectionPolicy = RoundRobinHostPolicy()
	session := &Session{
		cfg: *config,
		connCfg: &ConnConfig{
			Timeout:   10 * time.Millisecond,
			Keepalive: 0,
		},
		policy: config.PoolConfig.HostSelectionPolicy,
	}
	session.pool = config.PoolConfig.buildPool(session)
	return session
}

func createFunctions(t *testing.T, session *Session) {
	if err := session.Query(`
		CREATE OR REPLACE FUNCTION gocql_test.avgState ( state tuple<int,bigint>, val int )
		CALLED ON NULL INPUT
		RETURNS tuple<int,bigint>
		LANGUAGE java AS
		$$if (val !=null) {state.setInt(0, state.getInt(0)+1); state.setLong(1, state.getLong(1)+val.intValue());}return state;$$;	`).Exec(); err != nil {
		t.Fatalf("failed to create function with err: %v", err)
	}
	if err := session.Query(`
		CREATE OR REPLACE FUNCTION gocql_test.avgFinal ( state tuple<int,bigint> )
		CALLED ON NULL INPUT
		RETURNS double
		LANGUAGE java AS
		$$double r = 0; if (state.getInt(0) == 0) return null; r = state.getLong(1); r/= state.getInt(0); return Double.valueOf(r);$$ 
	`).Exec(); err != nil {
		t.Fatalf("failed to create function with err: %v", err)
	}
}

func createAggregate(t *testing.T, session *Session) {
	createFunctions(t, session)
	if err := session.Query(`
		CREATE OR REPLACE AGGREGATE gocql_test.average(int)
		SFUNC avgState
		STYPE tuple<int,bigint>
		FINALFUNC avgFinal
		INITCOND (0,0);
	`).Exec(); err != nil {
		t.Fatalf("failed to create aggregate with err: %v", err)
	}
}

func staticAddressTranslator(newAddr net.IP, newPort int) AddressTranslator {
	return AddressTranslatorFunc(func(addr net.IP, port int) (net.IP, int) {
		return newAddr, newPort
	})
}

func assertTrue(t *testing.T, description string, value bool) {
	if !value {
		t.Errorf("expected %s to be true", description)
	}
}

func assertEqual(t *testing.T, description string, expected, actual interface{}) {
	if expected != actual {
		t.Errorf("expected %s to be (%+v) but was (%+v) instead", description, expected, actual)
	}
}

func assertNil(t *testing.T, description string, actual interface{}) {
	if actual != nil {
		t.Errorf("expected %s to be (nil) but was (%+v) instead", description, actual)
	}
}

func assertNotNil(t *testing.T, description string, actual interface{}) {
	if actual == nil {
		t.Errorf("expected %s not to be (nil)", description)
	}
}
