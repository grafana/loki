/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2016, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

// Package gocql implements a fast and robust Cassandra driver for the
// Go programming language.
//
// # Upgrading to a new major version
//
// For detailed migration instructions between major versions, see the [upgrade guide].
//
// # Connecting to the cluster
//
// Pass a list of initial node IP addresses to [NewCluster] to create a new cluster configuration:
//
//	cluster := gocql.NewCluster("192.168.1.1", "192.168.1.2", "192.168.1.3")
//
// Port can be specified as part of the address, the above is equivalent to:
//
//	cluster := gocql.NewCluster("192.168.1.1:9042", "192.168.1.2:9042", "192.168.1.3:9042")
//
// It is recommended to use the value set in the Cassandra config for broadcast_address or listen_address,
// an IP address not a domain name. This is because events from Cassandra will use the configured IP
// address, which is used to index connected hosts. If the domain name specified resolves to more than 1 IP address
// then the driver may connect multiple times to the same host, and will not mark the node being down or up from events.
//
// Then you can customize more options (see [ClusterConfig]):
//
//	cluster.Keyspace = "example"
//	cluster.Consistency = gocql.Quorum
//	cluster.ProtoVersion = 4
//
// The driver tries to automatically detect the protocol version to use if not set, but you might want to set the
// protocol version explicitly, as it's not defined which version will be used in certain situations (for example
// during upgrade of the cluster when some of the nodes support different set of protocol versions than other nodes).
//
// Native protocol versions 3, 4, and 5 are supported.
// For features like per-query keyspace setting and timestamp override, use native protocol version 5.
//
// The driver advertises the module name and version in the STARTUP message, so servers are able to detect the version.
// If you use replace directive in go.mod, the driver will send information about the replacement module instead.
//
// When ready, create a session from the configuration. Don't forget to [Session.Close] the session once you are done with it:
//
//	session, err := cluster.CreateSession()
//	if err != nil {
//		return err
//	}
//	defer session.Close()
//
// # Reconnection and Host Recovery
//
// The driver provides robust reconnection mechanisms to handle network failures and host outages.
// Two main configuration settings control reconnection behavior:
//
//   - ClusterConfig.ReconnectionPolicy: Controls retry behavior for immediate connection failures, query-driven reconnection, and background recovery
//   - ClusterConfig.ReconnectInterval: Controls background recovery of DOWN hosts
//
// [ReconnectionPolicy] controls retry behavior for immediate connection failures, query-driven reconnection, and background recovery.
//
// [ConstantReconnectionPolicy] provides predictable fixed intervals (Default):
//
//	cluster.ReconnectionPolicy = &gocql.ConstantReconnectionPolicy{
//	    MaxRetries: 3,               // Maximum retry attempts
//	    Interval:   1 * time.Second, // Fixed interval between retries
//	}
//
// [ExponentialReconnectionPolicy] provides gentler backoff with capped intervals:
//
//	cluster.ReconnectionPolicy = &gocql.ExponentialReconnectionPolicy{
//	    MaxRetries:      5,                // 6 total attempts: 0+1+2+4+8+15 = 30s total
//	    InitialInterval: 1 * time.Second,  // Initial retry interval
//	    MaxInterval:     15 * time.Second, // Maximum retry interval (prevents excessive delays)
//	}
//
// Note: Each reconnection attempt sequence starts fresh from InitialInterval.
// This applies both to immediate connection failures and each ClusterConfig.ReconnectInterval cycle.
// For example, if ClusterConfig.ReconnectInterval=60s, every 60 seconds the background process
// starts a new sequence beginning at InitialInterval, not continuing from where
// the previous 60-second cycle ended.
//
// ClusterConfig.ReconnectInterval controls background recovery of DOWN hosts. When a host is marked DOWN, this process periodically
// attempts reconnection using the same ReconnectionPolicy settings:
//
//	cluster.ReconnectInterval = 60 * time.Second  // Check DOWN hosts every 60 seconds (default)
//
// Setting ClusterConfig.ReconnectInterval to 0 disables background reconnection.
//
// The reconnection process involves several components working together in a specific sequence:
//
//  1. Individual Connection Reconnection - Immediate retry attempts for failed connections within UP hosts
//  2. Host State Management - Marking hosts DOWN when all connections fail and retries are exhausted
//  3. Background Recovery - Periodic reconnection attempts for DOWN hosts via ReconnectInterval
//
// Individual connection reconnection occurs when connections fail within a host's pool, and the driver immediately attempts
// reconnection using ReconnectionPolicy. For hosts that remain UP (with working connections), failed individual connections
// are reconnected on a query-driven basis - every query execution triggers asynchronous reconnection attempts for missing
// connections. Queries proceed immediately using available connections while reconnection happens asynchronously in the
// background. There is no query latency impact from reconnection attempts. Multiple concurrent queries to the same host
// will not trigger parallel reconnection attempts - the driver uses a "filling" flag to ensure only one reconnection process runs per host.
//
// Host state management determines when a host is marked DOWN. Only when ALL connections to a host fail and ReconnectionPolicy
// retries are exhausted does the host get marked DOWN. DOWN hosts are excluded from query routing. Since DOWN hosts don't
// receive queries, they cannot benefit from query-driven reconnection. This is why the background ClusterConfig.ReconnectInterval process
// is essential for DOWN host recovery.
//
// Background recovery through ClusterConfig.ReconnectInterval periodically attempts to reconnect DOWN hosts using ReconnectionPolicy settings.
// Event-driven recovery also triggers immediate reconnection when Cassandra sends STATUS_CHANGE UP events.
//
// The complete recovery process follows these steps:
//
//  1. Connection fails → ReconnectionPolicy immediate retry attempts
//  2. Query-driven recovery → Each query to partially-failed hosts triggers reconnection attempts
//  3. Host marked DOWN → All connections failed and retries exhausted
//  4. Background recovery → ClusterConfig.ReconnectInterval process attempts reconnection using ReconnectionPolicy
//  5. Event recovery → Cassandra events can trigger immediate reconnection
//
// Here's a practical example showing how the settings work together:
//
//	cluster.ReconnectionPolicy = &gocql.ExponentialReconnectionPolicy{
//	    MaxRetries:      8,                // 9 total attempts (0s, 1s, 2s, 4s, 8s, 16s, 30s, 30s, 30s)
//	    InitialInterval: 1 * time.Second,  // Starts at 1 second
//	    MaxInterval:     30 * time.Second, // Caps exponential growth at 30 seconds
//	}
//
//	cluster.ReconnectInterval = 60 * time.Second  // Background checks every 60 seconds
//
// Timeline Example: With this configuration, when a host loses ALL connections:
//
//	T=0:00      - Host has 2 connections, both fail
//	T=0:00      - Immediate reconnection attempt 1: 0s delay
//	T=0:01      - Immediate reconnection attempt 2: 1s delay
//	T=0:03      - Immediate reconnection attempt 3: 2s delay
//	T=0:07      - Immediate reconnection attempt 4: 4s delay
//	T=0:15      - Immediate reconnection attempt 5: 8s delay
//	T=0:31      - Immediate reconnection attempt 6: 16s delay
//	T=1:01      - Immediate reconnection attempt 7: 30s delay (capped by MaxInterval)
//	T=1:31      - Immediate reconnection attempt 8: 30s delay
//	T=2:01      - Immediate reconnection attempt 9: 30s delay
//	T=2:31      - All immediate attempts failed, host marked DOWN
//
//	T=3:31      - Background recovery attempt 1 starts (60s after DOWN)
//	            ReconnectionPolicy sequence: 0s, 1s, 2s, 4s, 8s, 16s, 30s, 30s, 30s
//
//	T=4:31      - ClusterConfig.ReconnectInterval timer fires, tick buffered (timer channel capacity=1)
//	T=5:31      - ClusterConfig.ReconnectInterval timer fires again, there is already a tick buffered so ignore
//	T=5:32      - Background recovery attempt 1 completes (after 2:01), immediately reads buffered tick
//	T=5:32      - Background recovery attempt 2 starts (buffered timer from T=5:31)
//	T=6:32      - ClusterConfig.ReconnectInterval timer fires, tick buffered
//	T=7:32      - ClusterConfig.ReconnectInterval timer fires again, there is already a tick buffered so ignore
//	T=7:33      - Background recovery attempt 2 completes (after 2:01), immediately reads buffered tick
//	T=7:33      - Background recovery attempt 3 starts (buffered timer from T=7:32)
//
// Timer Behavior and Predictable Timing:
//
// Note: [time.Ticker].C has buffer capacity=1, but Go drops ticks for "slow receivers."
// The reconnection process is a slow receiver (taking 2+ minutes vs 60s interval).
// First missed tick gets buffered, subsequent ticks are dropped. When reconnection
// completes, it immediately reads the buffered tick and starts the next attempt.
// This causes attempts to run back-to-back at the ReconnectionPolicy duration interval
// (121s) instead of the intended ClusterConfig.ReconnectInterval (60s), but timing remains predictable.
//
// To avoid this buffering/dropping behavior, ensure ClusterConfig.ReconnectInterval is larger than the
// total ReconnectionPolicy duration. You can achieve this by either:
//
//  1. Increasing ClusterConfig.ReconnectInterval (e.g., 150s > 121s sequence duration)
//  2. Reducing ReconnectionPolicy duration (e.g., 30s sequence < 60s ClusterConfig.ReconnectInterval)
//
// This ensures predictable timing with each recovery attempt starting exactly ClusterConfig.ReconnectInterval apart.
// Approach #2 provides faster recovery while maintaining predictable timing.
//
// Individual failed connections within UP hosts are reconnected asynchronously without affecting query performance.
//
// Best Practices and Configuration Guidelines:
//
//   - ReconnectionPolicy: Use ConstantReconnectionPolicy for predictable behavior or ExponentialReconnectionPolicy
//     for gentler recovery. Aggressive settings affect background reconnection frequency but don't impact query latency
//   - ClusterConfig.ReconnectInterval: Set to 30-60 seconds for most cases. Shorter intervals provide faster recovery but more traffic
//   - Timing Predictability: For predictable background recovery timing, ensure ClusterConfig.ReconnectInterval exceeds the total
//     ReconnectionPolicy sequence duration. This prevents Go's ticker from buffering/dropping ticks due to "slow receiver"
//     behavior. You can achieve this by either increasing ClusterConfig.ReconnectInterval or reducing ReconnectionPolicy duration
//     (fewer retries/shorter intervals). The latter approach provides faster recovery while maintaining predictable timing
//   - Monitoring: Enable logging to observe reconnection behavior and tune settings
//
// # Compression
//
// The driver supports Snappy and LZ4 compression of protocol frames.
//
// For Snappy compression (via [github.com/apache/cassandra-gocql-driver/v2/snappy] package):
//
//	import "github.com/apache/cassandra-gocql-driver/v2/snappy"
//
//	cluster.Compressor = &snappy.SnappyCompressor{}
//
// For LZ4 compression (via [github.com/apache/cassandra-gocql-driver/v2/lz4] package):
//
//	import "github.com/apache/cassandra-gocql-driver/v2/lz4"
//
//	cluster.Compressor = &lz4.LZ4Compressor{}
//
// Both compressors use efficient append-like semantics for optimal performance and memory usage.
//
// # Structured Logging
//
// The driver provides structured logging through the [StructuredLogger] interface.
// Built-in integrations are available for popular logging libraries:
//
// For Zap logger (via [github.com/apache/cassandra-gocql-driver/v2/gocqlzap] package):
//
//	import "github.com/apache/cassandra-gocql-driver/v2/gocqlzap"
//
//	zapLogger, _ := zap.NewProduction()
//	cluster.Logger = gocqlzap.NewZapLogger(zapLogger)
//
// For Zerolog (via [github.com/apache/cassandra-gocql-driver/v2/gocqlzerolog] package):
//
//	import "github.com/apache/cassandra-gocql-driver/v2/gocqlzerolog"
//
//	zerologLogger := zerolog.New(os.Stdout).With().Timestamp().Logger()
//	cluster.Logger = gocqlzerolog.NewZerologLogger(&zerologLogger)
//
// You can also use the built-in standard library logger:
//
//	cluster.Logger = gocql.NewLogger(gocql.LogLevelInfo)
//
// # Native Protocol Version 5 Features
//
// Native protocol version 5 provides several advanced capabilities:
//
// Set keyspace for individual queries (useful for multi-tenant applications):
//
//	err := session.Query("SELECT * FROM table").SetKeyspace("tenant1").Exec()
//
// Target queries to specific nodes (useful for virtual tables in Cassandra 4.0+):
//
//	err := session.Query("SELECT * FROM system_views.settings").
//		SetHostID("host-uuid").Exec()
//
// Use current timestamp override for testing and consistency:
//
//	err := session.Query("INSERT INTO table (id, data) VALUES (?, ?)").
//		WithNowInSeconds(specificTimestamp).
//		Bind(id, data).Exec()
//
// These features are also available on batch operations, excluding SetHostID():
//
//	err := session.Batch(LoggedBatch).
//		Query("INSERT INTO table (id, data) VALUES (?, ?)", id, data).
//		SetKeyspace("tenant1").
//		WithNowInSeconds(specificTimestamp).
//		Exec()
//
// # Authentication
//
// CQL protocol uses a SASL-based authentication mechanism and so consists of an exchange of server challenges and
// client response pairs. The details of the exchanged messages depend on the authenticator used.
//
// To use authentication, set ClusterConfig.Authenticator or ClusterConfig.AuthProvider.
//
// [PasswordAuthenticator] is provided to use for username/password authentication:
//
//	 cluster := gocql.NewCluster("192.168.1.1", "192.168.1.2", "192.168.1.3")
//	 cluster.Authenticator = gocql.PasswordAuthenticator{
//			Username: "user",
//			Password: "password"
//	 }
//	 session, err := cluster.CreateSession()
//	 if err != nil {
//	 	return err
//	 }
//	 defer session.Close()
//
// By default, PasswordAuthenticator will attempt to authenticate regardless of what implementation the server returns
// in its AUTHENTICATE message as its authenticator, (e.g. org.apache.cassandra.auth.PasswordAuthenticator).  If you
// wish to restrict this you may use PasswordAuthenticator.AllowedAuthenticators:
//
//	 cluster.Authenticator = gocql.PasswordAuthenticator {
//			Username:              "user",
//			Password:              "password"
//			AllowedAuthenticators: []string{"org.apache.cassandra.auth.PasswordAuthenticator"},
//	 }
//
// # Transport layer security
//
// It is possible to secure traffic between the client and server with TLS.
//
// To use TLS, set the ClusterConfig.SslOpts field. [SslOptions] embeds *[crypto/tls.Config] so you can set that directly.
// There are also helpers to load keys/certificates from files.
//
// Warning: Due to historical reasons, the SslOptions is insecure by default, so you need to set SslOptions.EnableHostVerification
// to true if no Config is set. Most users should set SslOptions.Config to a *crypto/tls.Config.
// SslOptions and crypto/tls.Config.InsecureSkipVerify interact as follows:
//
//	Config.InsecureSkipVerify | EnableHostVerification | Result
//	Config is nil             | false                  | do not verify host
//	Config is nil             | true                   | verify host
//	false                     | false                  | verify host
//	true                      | false                  | do not verify host
//	false                     | true                   | verify host
//	true                      | true                   | verify host
//
// For example:
//
//	cluster := gocql.NewCluster("192.168.1.1", "192.168.1.2", "192.168.1.3")
//	cluster.SslOpts = &gocql.SslOptions{
//		EnableHostVerification: true,
//	}
//	session, err := cluster.CreateSession()
//	if err != nil {
//		return err
//	}
//	defer session.Close()
//
// # Data-center awareness and query routing
//
// To route queries to local DC first, use [DCAwareRoundRobinPolicy]. For example, if the datacenter you
// want to primarily connect is called dc1 (as configured in the database):
//
//	cluster := gocql.NewCluster("192.168.1.1", "192.168.1.2", "192.168.1.3")
//	cluster.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy("dc1")
//
// The driver can route queries to nodes that hold data replicas based on partition key (preferring local DC).
//
//	cluster := gocql.NewCluster("192.168.1.1", "192.168.1.2", "192.168.1.3")
//	cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.DCAwareRoundRobinPolicy("dc1"))
//
// Note that [TokenAwareHostPolicy] can take options such as [ShuffleReplicas] and [NonLocalReplicasFallback].
//
// We recommend running with a token aware host policy in production for maximum performance.
//
// The driver can only use token-aware routing for queries where all partition key columns are query parameters.
// For example, instead of
//
//	session.Query("select value from mytable where pk1 = 'abc' AND pk2 = ?", "def")
//
// use
//
//	session.Query("select value from mytable where pk1 = ? AND pk2 = ?", "abc", "def")
//
// # Rack-level awareness
//
// The DCAwareRoundRobinPolicy can be replaced with [RackAwareRoundRobinPolicy], which takes two parameters, datacenter and rack.
//
// Instead of dividing hosts with two tiers (local datacenter and remote datacenters) it divides hosts into three
// (the local rack, the rest of the local datacenter, and everything else).
//
// RackAwareRoundRobinPolicy can be combined with TokenAwareHostPolicy in the same way as DCAwareRoundRobinPolicy.
//
// # Executing queries
//
// Create queries with [Session.Query]. Query values must not be reused between different executions and must not be
// modified after starting execution of the query.
//
// To execute a query without reading results, use [Query.Exec]:
//
//	 err := session.Query(`INSERT INTO tweet (timeline, id, text) VALUES (?, ?, ?)`,
//			"me", gocql.TimeUUID(), "hello world").WithContext(ctx).Exec()
//
// Single row can be read by calling [Query.Scan]:
//
//	 err := session.Query(`SELECT id, text FROM tweet WHERE timeline = ? LIMIT 1`,
//			"me").WithContext(ctx).Consistency(gocql.One).Scan(&id, &text)
//
// Multiple rows can be read using [Iter.Scanner]:
//
//	 scanner := session.Query(`SELECT id, text FROM tweet WHERE timeline = ?`,
//	 	"me").WithContext(ctx).Iter().Scanner()
//	 for scanner.Next() {
//	 	var (
//	 		id gocql.UUID
//			text string
//	 	)
//	 	err = scanner.Scan(&id, &text)
//	 	if err != nil {
//	 		log.Fatal(err)
//	 	}
//	 	fmt.Println("Tweet:", id, text)
//	 }
//	 // scanner.Err() closes the iterator, so scanner nor iter should be used afterwards.
//	 if err := scanner.Err(); err != nil {
//	 	log.Fatal(err)
//	 }
//
// See Example for complete example.
//
// # Vector types (Cassandra 5.0+)
//
// The driver supports Cassandra 5.0 vector types, enabling powerful vector search capabilities:
//
//	// Create a table with vector column
//	err := session.Query(`CREATE TABLE vectors (
//		id int PRIMARY KEY,
//		embedding vector<float, 128>
//	)`).Exec()
//
//	// Insert vector data
//	embedding := make([]float32, 128)
//	// ... populate embedding values
//	err = session.Query("INSERT INTO vectors (id, embedding) VALUES (?, ?)",
//		1, embedding).Exec()
//
//	// Query vector data
//	var retrievedEmbedding []float32
//	err = session.Query("SELECT embedding FROM vectors WHERE id = ?", 1).
//		Scan(&retrievedEmbedding)
//
// Vector types support various element types including basic types, collections, and user-defined types.
// Vector search requires Cassandra 5.0 or later.
//
// # Prepared statements
//
// The driver automatically prepares DML queries (SELECT/INSERT/UPDATE/DELETE/BATCH statements) and maintains a cache
// of prepared statements.
// CQL protocol does not support preparing other query types.
//
// When using native protocol >= 4, it is possible to use [UnsetValue] as the bound value of a column.
// This will cause the database to ignore writing the column.
// The main advantage is the ability to keep the same prepared statement even when you don't
// want to update some fields, where before you needed to make another prepared statement.
//
// # Executing multiple queries concurrently
//
// [Session] is safe to use from multiple goroutines, so to execute multiple concurrent queries, just execute them
// from several worker goroutines. Gocql provides synchronously-looking API (as recommended for Go APIs) and the queries
// are executed asynchronously at the protocol level.
//
//	results := make(chan error, 2)
//	go func() {
//		results <- session.Query(`INSERT INTO tweet (timeline, id, text) VALUES (?, ?, ?)`,
//			"me", gocql.TimeUUID(), "hello world 1").Exec()
//	}()
//	go func() {
//		results <- session.Query(`INSERT INTO tweet (timeline, id, text) VALUES (?, ?, ?)`,
//			"me", gocql.TimeUUID(), "hello world 2").Exec()
//	}()
//
// # Nulls
//
// Null values are are unmarshalled as zero value of the type. If you need to distinguish for example between text
// column being null and empty string, you can unmarshal into *string variable instead of string.
//
//	var text *string
//	err := scanner.Scan(&text)
//	if err != nil {
//		// handle error
//	}
//	if text != nil {
//		// not null
//	}
//	else {
//		// null
//	}
//
// See Example_nulls for full example.
//
// # Reusing slices
//
// The driver reuses backing memory of slices when unmarshalling. This is an optimization so that a buffer does not
// need to be allocated for every processed row. However, you need to be careful when storing the slices to other
// memory structures.
//
//	scanner := session.Query(`SELECT myints FROM table WHERE pk = ?`, "key").WithContext(ctx).Iter().Scanner()
//	var myInts []int
//	for scanner.Next() {
//		// This scan reuses backing store of myInts for each row.
//		err = scanner.Scan(&myInts)
//		if err != nil {
//			log.Fatal(err)
//		}
//	}
//
// When you want to save the data for later use, pass a new slice every time. A common pattern is to declare the
// slice variable within the scanner loop:
//
//	scanner := session.Query(`SELECT myints FROM table WHERE pk = ?`, "key").WithContext(ctx).Iter().Scanner()
//	for scanner.Next() {
//		var myInts []int
//		// This scan always gets pointer to fresh myInts slice, so does not reuse memory.
//		err = scanner.Scan(&myInts)
//		if err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # Paging
//
// The driver supports paging of results with automatic prefetch, see ClusterConfig.PageSize,
// [Query.PageSize], and [Query.Prefetch].
//
// It is also possible to control the paging manually with Query.PageState (this disables automatic prefetch).
// Manual paging is useful if you want to store the page state externally, for example in a URL to allow users
// browse pages in a result. You might want to sign/encrypt the paging state when exposing it externally since
// it contains data from primary keys.
//
// Paging state is specific to the native protocol version and the exact query used. It is meant as opaque state that
// should not be modified. If you send paging state from different query or protocol version, then the behaviour
// is not defined (you might get unexpected results or an error from the server). For example, do not send paging state
// returned by node using protocol version 3 to a node using protocol version 4. Also, when using protocol version 4,
// paging state between Cassandra 2.2 and 3.0 is incompatible (see [CASSANDRA-10880]).
//
// The driver does not check whether the paging state is from the same protocol version/statement.
// You might want to validate yourself as this could be a problem if you store paging state externally.
// For example, if you store paging state in a URL, the URLs might become broken when you upgrade your cluster.
//
// Call Query.PageState(nil) to fetch just the first page of the query results. Pass the page state returned by
// [Iter.PageState] to Query.PageState of a subsequent query to get the next page. If the length of slice returned
// by Iter.PageState is zero, there are no more pages available (or an error occurred).
//
// Using too low values of ClusterConfig.PageSize will negatively affect performance, a value below 100 is probably too low.
// While Cassandra returns exactly ClusterConfig.PageSize items (except for last page) in a page currently, the protocol authors
// explicitly reserved the right to return smaller or larger amount of items in a page for performance reasons, so don't
// rely on the page having the exact count of items.
//
// See Example_paging for an example of manual paging.
//
// # Dynamic list of columns
//
// There are certain situations when you don't know the list of columns in advance, mainly when the query is supplied
// by the user. [Iter.Columns], Iter.RowData, Iter.MapScan and Iter.SliceMap can be used to handle this case.
//
// See Example_dynamicColumns.
//
// # Batches
//
// The CQL protocol supports sending batches of DML statements (INSERT/UPDATE/DELETE) and so does gocql.
// Use [Session.Batch] to create a new batch and then fill-in details of individual queries.
// Then execute the batch with [Batch.Exec].
//
// Logged batches ensure atomicity, either all or none of the operations in the batch will succeed, but they have
// overhead to ensure this property.
// Unlogged batches don't have the overhead of logged batches, but don't guarantee atomicity.
// Updates of counters are handled specially by Cassandra so batches of counter updates have to use [CounterBatch] type.
// A counter batch can only contain statements to update counters.
//
// For unlogged batches it is recommended to send only single-partition batches (i.e. all statements in the batch should
// involve only a single partition).
// Multi-partition batch needs to be split by the coordinator node and re-sent to
// correct nodes.
// With single-partition batches you can send the batch directly to the node for the partition without incurring the
// additional network hop.
//
// It is also possible to pass entire BEGIN BATCH .. APPLY BATCH statement to [Query.Exec].
// There are differences how those are executed.
// BEGIN BATCH statement passed to Query.Exec is prepared as a whole in a single statement.
// Batch.Exec prepares individual statements in the batch.
// If you have variable-length batches using the same statement, using Batch.Exec is more efficient.
//
// See Example_batch for an example.
//
// The [Batch] API provides a fluent interface for building and executing batch operations:
//
//	// Create and execute a batch using fluent API
//	err := session.Batch(LoggedBatch).
//		Query("INSERT INTO table1 (id, name) VALUES (?, ?)", id1, name1).
//		Query("INSERT INTO table2 (id, value) VALUES (?, ?)", id2, value2).
//		Exec()
//
//	// Lightweight transactions with batches
//	applied, iter, err := session.Batch(LoggedBatch).
//		Query("INSERT INTO users (id, name) VALUES (?, ?) IF NOT EXISTS", id, name).
//		ExecCAS()
//	if err != nil {
//		// handle error
//	}
//	if !applied {
//		// handle conditional failure
//	}
//
// # Lightweight transactions
//
// [Query.ScanCAS] or [Query.MapScanCAS] can be used to execute a single-statement lightweight transaction (an
// INSERT/UPDATE .. IF statement) and reading its result. See example for Query.MapScanCAS.
//
// Multiple-statement lightweight transactions can be executed as a logged batch that contains at least one conditional
// statement. All the conditions must return true for the batch to be applied. You can use [Batch.ExecCAS] and
// [Batch.MapExecCAS] when executing the batch to learn about the result of the LWT. See example for
// Batch.MapExecCAS.
//
// # SERIAL Consistency for Reads
//
// The driver supports SERIAL and LOCAL_SERIAL consistency levels on SELECT statements.
// These special consistency levels are designed for reading data that may have been written using
// lightweight transactions (LWT) with IF conditions, providing linearizable consistency guarantees.
//
// When to use SERIAL consistency levels:
//
// Use SERIAL or LOCAL_SERIAL consistency when you need to:
//   - Read the most recent committed value after lightweight transactions
//   - Ensure linearizable consistency (stronger than eventual consistency)
//   - Read data that might have uncommitted lightweight transactions in progress
//
// Important considerations:
//
//   - SERIAL reads have higher latency and resource usage than normal reads
//   - Only use when you specifically need linearizable consistency
//   - If a SERIAL read finds an uncommitted transaction, it will commit that transaction
//   - Most applications should use regular consistency levels (ONE, QUORUM, etc.)
//
// # Immutable Execution
//
// [Query] and [Batch] objects follow an immutable execution model that enables safe reuse and concurrent
// execution without object mutation.
//
// Query and Batch Object Reusability:
//
// Query and Batch objects remain unchanged during execution, allowing for safe reuse and concurrent execution:
//
//	// Create a query once
//	query := session.Query("SELECT * FROM users WHERE id = ?", userID)
//
//	// Safe to execute multiple times
//	iter1 := query.Iter()
//	defer iter1.Close()
//
//	iter2 := query.Iter() // Same query, separate execution
//	defer iter2.Close()
//
//	// Safe to use from multiple goroutines
//	go func() {
//		iter := query.Iter()
//		defer iter.Close()
//		// ... process results
//	}()
//
// The same applies to Batch objects:
//
//	// Create batch once using fluent API
//	batch := session.Batch(LoggedBatch).
//		Query("INSERT INTO table1 (id, name) VALUES (?, ?)", id1, name1).
//		Query("INSERT INTO table2 (id, value) VALUES (?, ?)", id2, value2)
//
//	// Safe to execute multiple times
//	err1 := batch.Exec()
//	err2 := batch.Exec() // Same batch, separate execution
//
// Execution Metrics and Metadata:
//
// Execution-specific information such as metrics, attempts, and latency are available through the [Iter] object
// returned by execution. This provides per-execution metrics while keeping the original objects unchanged:
//
//	query := session.Query("SELECT * FROM users WHERE id = ?", userID)
//	iter := query.Iter()
//	defer iter.Close()
//
//	// Access execution metrics through Iter
//	attempts := iter.Attempts()        // Number of times this execution was attempted
//	latency := iter.Latency()          // Average latency per attempt in nanoseconds
//	keyspace := iter.Keyspace()        // Keyspace the query was executed against
//	table := iter.Table()              // Table the query was executed against (if determinable)
//	host := iter.Host()                // Host that executed the query
//
// For batches, execution methods that return an Iter (like ExecCAS) provide the same metrics:
//
//	// Execute CAS operation and get metrics through Iter using fluent API
//	applied, iter, err := session.Batch(LoggedBatch).
//		Query("INSERT INTO users (id, name) VALUES (?, ?) IF NOT EXISTS", id, name).
//		ExecCAS()
//	defer iter.Close()
//
//	if err != nil {
//		log.Printf("Batch failed after %d attempts with average latency %d ns",
//			iter.Attempts(), iter.Latency())
//	}
//
// # Retries and speculative execution
//
// Queries can be marked as idempotent. Marking the query as idempotent tells the driver that the query can be executed
// multiple times without affecting its result. Non-idempotent queries are not eligible for retrying nor speculative
// execution.
//
// Idempotent queries are retried in case of errors based on the configured RetryPolicy.
//
// Queries can be retried even before they fail by setting a SpeculativeExecutionPolicy. The policy can
// cause the driver to retry on a different node if the query is taking longer than a specified delay even before the
// driver receives an error or timeout from the server. When a query is speculatively executed, the original execution
// is still executing. The two parallel executions of the query race to return a result, the first received result will
// be returned.
//
// # User-defined types
//
// Cassandra User-Defined Types (UDTs) are composite data types that group related fields together.
// GoCQL provides several ways to work with UDTs in Go, from simple struct mapping to advanced custom marshaling.
//
// Basic UDT Usage with Structs:
//
// The simplest way to work with UDTs is using Go structs with `cql` tags:
//
//	// Cassandra UDT definition:
//	// CREATE TYPE address (street text, city text, zip_code int);
//
//	type Address struct {
//		Street  string `cql:"street"`
//		City    string `cql:"city"`
//		ZipCode int    `cql:"zip_code"`
//	}
//
//	// Usage in queries
//	addr := Address{Street: "123 Main St", City: "Anytown", ZipCode: 12345}
//	err := session.Query("INSERT INTO users (id, address) VALUES (?, ?)",
//		userID, addr).Exec()
//
//	// Reading UDTs
//	var readAddr Address
//	err = session.Query("SELECT address FROM users WHERE id = ?",
//		userID).Scan(&readAddr)
//
// Field Mapping:
//
// GoCQL maps struct fields to UDT fields using two strategies:
//
// 1. CQL tags: Use `cql:"field_name"` to explicitly map fields
// 2. Name matching: If no tag is present, field names must match exactly (case-sensitive)
//
//	type MyUDT struct {
//		FieldA int32  `cql:"field_a"`  // Maps to UDT field "field_a"
//		FieldB string `cql:"field_b"`  // Maps to UDT field "field_b"
//		FieldC string                  // Maps to UDT field "FieldC" (exact name match)
//	}
//
// Working with Maps:
//
// UDTs can also be marshaled to/from `map[string]interface{}`:
//
//	// Marshal to map
//	var udtMap map[string]interface{}
//	err := session.Query("SELECT address FROM users WHERE id = ?",
//		userID).Scan(&udtMap)
//
//	// Access fields
//	street := udtMap["street"].(string)
//	zipCode := udtMap["zip_code"].(int)
//
// Advanced Custom Marshaling:
//
// For complex scenarios, implement the UDTMarshaler and UDTUnmarshaler interfaces:
//
//	type CustomUDT struct {
//		fieldA string
//		fieldB int32
//	}
//
//	// UDTMarshaler for writing to Cassandra
//	func (c CustomUDT) MarshalUDT(name string, info TypeInfo) ([]byte, error) {
//		switch name {
//		case "field_a":
//			return Marshal(info, c.fieldA)
//		case "field_b":
//			return Marshal(info, c.fieldB)
//		default:
//			return nil, nil // Unknown fields set to null
//		}
//	}
//
//	// UDTUnmarshaler for reading from Cassandra
//	func (c *CustomUDT) UnmarshalUDT(name string, info TypeInfo, data []byte) error {
//		switch name {
//		case "field_a":
//			return Unmarshal(info, data, &c.fieldA)
//		case "field_b":
//			return Unmarshal(info, data, &c.fieldB)
//		default:
//			return nil // Ignore unknown fields for forward compatibility
//		}
//	}
//
// Nested UDTs and Collections:
//
// UDTs can contain other UDTs and collection types:
//
//	// Cassandra definitions:
//	// CREATE TYPE address (street text, city text);
//	// CREATE TYPE person (name text, addresses list<frozen<address>>);
//
//	type Person struct {
//		Name      string    `cql:"name"`
//		Addresses []Address `cql:"addresses"`
//	}
//
// See Example_userDefinedTypesMap, Example_userDefinedTypesStruct, ExampleUDTMarshaler, ExampleUDTUnmarshaler.
//
// # Metrics and tracing
//
// It is possible to provide observer implementations that could be used to gather metrics:
//
//   - [QueryObserver] for monitoring individual queries.
//   - [BatchObserver] for monitoring batch queries.
//   - [ConnectObserver] for monitoring new connections from the driver to the database.
//   - [FrameHeaderObserver] for monitoring individual protocol frames.
//
// CQL protocol also supports tracing of queries. When enabled, the database will write information about
// internal events that happened during execution of the query. You can use [Query.Trace] to request tracing and receive
// the session ID that the database used to store the trace information in system_traces.sessions and
// system_traces.events tables. [NewTraceWriter] returns an implementation of [Tracer] that writes the events to a writer.
// Gathering trace information might be essential for debugging and optimizing queries, but writing traces has overhead,
// so this feature should not be used on production systems with very high load unless you know what you are doing.
//
// [upgrade guide]: https://github.com/apache/cassandra-gocql-driver/blob/trunk/UPGRADE_GUIDE.md
// [CASSANDRA-10880]: https://issues.apache.org/jira/browse/CASSANDRA-10880
package gocql // import "github.com/apache/cassandra-gocql-driver/v2"
