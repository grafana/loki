// +build all cassandra

package gocql

import (
	"testing"
	"time"
)

func TestBatch_Errors(t *testing.T) {
	if *flagProto == 1 {
	}

	session := createSession(t)
	defer session.Close()

	if session.cfg.ProtoVersion < protoVersion2 {
		t.Skip("atomic batches not supported. Please use Cassandra >= 2.0")
	}

	if err := createTable(session, `CREATE TABLE gocql_test.batch_errors (id int primary key, val inet)`); err != nil {
		t.Fatal(err)
	}

	b := session.NewBatch(LoggedBatch)
	b.Query("SELECT * FROM batch_errors WHERE id=2 AND val=?", nil)
	if err := session.ExecuteBatch(b); err == nil {
		t.Fatal("expected to get error for invalid query in batch")
	}
}

func TestBatch_WithTimestamp(t *testing.T) {
	session := createSession(t)
	defer session.Close()

	if session.cfg.ProtoVersion < protoVersion3 {
		t.Skip("Batch timestamps are only available on protocol >= 3")
	}

	if err := createTable(session, `CREATE TABLE gocql_test.batch_ts (id int primary key, val text)`); err != nil {
		t.Fatal(err)
	}

	micros := time.Now().UnixNano()/1e3 - 1000

	b := session.NewBatch(LoggedBatch)
	b.WithTimestamp(micros)
	b.Query("INSERT INTO batch_ts (id, val) VALUES (?, ?)", 1, "val")
	if err := session.ExecuteBatch(b); err != nil {
		t.Fatal(err)
	}

	var storedTs int64
	if err := session.Query(`SELECT writetime(val) FROM batch_ts WHERE id = ?`, 1).Scan(&storedTs); err != nil {
		t.Fatal(err)
	}

	if storedTs != micros {
		t.Errorf("got ts %d, expected %d", storedTs, micros)
	}
}
