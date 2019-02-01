// +build ccm, ignore

package gocql

import (
	"log"
	"testing"
	"time"

	"github.com/gocql/gocql/internal/ccm"
)

func TestEventDiscovery(t *testing.T) {
	t.Skip("FLAKE skipping")
	if err := ccm.AllUp(); err != nil {
		t.Fatal(err)
	}

	session := createSession(t)
	defer session.Close()

	status, err := ccm.Status()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status=%+v\n", status)

	session.pool.mu.RLock()
	poolHosts := session.pool.hostConnPools // TODO: replace with session.ring
	t.Logf("poolhosts=%+v\n", poolHosts)
	// check we discovered all the nodes in the ring
	for _, host := range status {
		if _, ok := poolHosts[host.Addr]; !ok {
			t.Errorf("did not discover %q", host.Addr)
		}
	}
	session.pool.mu.RUnlock()
	if t.Failed() {
		t.FailNow()
	}
}

func TestEventNodeDownControl(t *testing.T) {
	t.Skip("FLAKE skipping")
	const targetNode = "node1"
	if err := ccm.AllUp(); err != nil {
		t.Fatal(err)
	}

	status, err := ccm.Status()
	if err != nil {
		t.Fatal(err)
	}

	cluster := createCluster()
	cluster.Hosts = []string{status[targetNode].Addr}
	session := createSessionFromCluster(cluster, t)
	defer session.Close()

	t.Log("marking " + targetNode + " as down")
	if err := ccm.NodeDown(targetNode); err != nil {
		t.Fatal(err)
	}

	t.Logf("status=%+v\n", status)
	t.Logf("marking node %q down: %v\n", targetNode, status[targetNode])

	time.Sleep(5 * time.Second)

	session.pool.mu.RLock()

	poolHosts := session.pool.hostConnPools
	node := status[targetNode]
	t.Logf("poolhosts=%+v\n", poolHosts)

	if _, ok := poolHosts[node.Addr]; ok {
		session.pool.mu.RUnlock()
		t.Fatal("node not removed after remove event")
	}
	session.pool.mu.RUnlock()

	host := session.ring.getHost(node.Addr)
	if host == nil {
		t.Fatal("node not in metadata ring")
	} else if host.IsUp() {
		t.Fatalf("not not marked as down after event in metadata: %v", host)
	}
}

func TestEventNodeDown(t *testing.T) {
	t.Skip("FLAKE skipping")
	const targetNode = "node3"
	if err := ccm.AllUp(); err != nil {
		t.Fatal(err)
	}

	session := createSession(t)
	defer session.Close()

	if err := ccm.NodeDown(targetNode); err != nil {
		t.Fatal(err)
	}

	status, err := ccm.Status()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("status=%+v\n", status)
	t.Logf("marking node %q down: %v\n", targetNode, status[targetNode])

	time.Sleep(5 * time.Second)

	session.pool.mu.RLock()
	defer session.pool.mu.RUnlock()

	poolHosts := session.pool.hostConnPools
	node := status[targetNode]
	t.Logf("poolhosts=%+v\n", poolHosts)

	if _, ok := poolHosts[node.Addr]; ok {
		t.Fatal("node not removed after remove event")
	}

	host := session.ring.getHost(node.Addr)
	if host == nil {
		t.Fatal("node not in metadata ring")
	} else if host.IsUp() {
		t.Fatalf("not not marked as down after event in metadata: %v", host)
	}
}

func TestEventNodeUp(t *testing.T) {
	t.Skip("FLAKE skipping")
	if err := ccm.AllUp(); err != nil {
		t.Fatal(err)
	}

	status, err := ccm.Status()
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("status=%+v\n", status)

	session := createSession(t)
	defer session.Close()

	const targetNode = "node2"
	node := status[targetNode]

	_, ok := session.pool.getPool(node.Addr)
	if !ok {
		session.pool.mu.RLock()
		t.Errorf("target pool not in connection pool: addr=%q pools=%v", status[targetNode].Addr, session.pool.hostConnPools)
		session.pool.mu.RUnlock()
		t.FailNow()
	}

	if err := ccm.NodeDown(targetNode); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Second)

	_, ok = session.pool.getPool(node.Addr)
	if ok {
		t.Fatal("node not removed after remove event")
	}

	if err := ccm.NodeUp(targetNode); err != nil {
		t.Fatal(err)
	}

	// cassandra < 2.2 needs 10 seconds to start up the binary service
	time.Sleep(15 * time.Second)

	_, ok = session.pool.getPool(node.Addr)
	if !ok {
		t.Fatal("node not added after node added event")
	}

	host := session.ring.getHost(node.Addr)
	if host == nil {
		t.Fatal("node not in metadata ring")
	} else if !host.IsUp() {
		t.Fatalf("not not marked as UP after event in metadata: addr=%q host=%p: %v", node.Addr, host, host)
	}
}

func TestEventFilter(t *testing.T) {
	t.Skip("FLAKE skipping")
	if err := ccm.AllUp(); err != nil {
		t.Fatal(err)
	}

	status, err := ccm.Status()
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("status=%+v\n", status)

	cluster := createCluster()
	cluster.HostFilter = WhiteListHostFilter(status["node1"].Addr)
	session := createSessionFromCluster(cluster, t)
	defer session.Close()

	if _, ok := session.pool.getPool(status["node1"].Addr); !ok {
		t.Errorf("should have %v in pool but dont", "node1")
	}

	for _, host := range [...]string{"node2", "node3"} {
		_, ok := session.pool.getPool(status[host].Addr)
		if ok {
			t.Errorf("should not have %v in pool", host)
		}
	}

	if t.Failed() {
		t.FailNow()
	}

	if err := ccm.NodeDown("node2"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Second)

	if err := ccm.NodeUp("node2"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(15 * time.Second)
	for _, host := range [...]string{"node2", "node3"} {
		_, ok := session.pool.getPool(status[host].Addr)
		if ok {
			t.Errorf("should not have %v in pool", host)
		}
	}

	if t.Failed() {
		t.FailNow()
	}

}

func TestEventDownQueryable(t *testing.T) {
	t.Skip("FLAKE skipping")
	if err := ccm.AllUp(); err != nil {
		t.Fatal(err)
	}

	status, err := ccm.Status()
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("status=%+v\n", status)

	const targetNode = "node1"

	addr := status[targetNode].Addr

	cluster := createCluster()
	cluster.Hosts = []string{addr}
	cluster.HostFilter = WhiteListHostFilter(addr)
	session := createSessionFromCluster(cluster, t)
	defer session.Close()

	if pool, ok := session.pool.getPool(addr); !ok {
		t.Fatalf("should have %v in pool but dont", addr)
	} else if !pool.host.IsUp() {
		t.Fatalf("host is not up %v", pool.host)
	}

	if err := ccm.NodeDown(targetNode); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Second)

	if err := ccm.NodeUp(targetNode); err != nil {
		t.Fatal(err)
	}

	time.Sleep(15 * time.Second)

	if pool, ok := session.pool.getPool(addr); !ok {
		t.Fatalf("should have %v in pool but dont", addr)
	} else if !pool.host.IsUp() {
		t.Fatalf("host is not up %v", pool.host)
	}

	var rows int
	if err := session.Query("SELECT COUNT(*) FROM system.local").Scan(&rows); err != nil {
		t.Fatal(err)
	} else if rows != 1 {
		t.Fatalf("expected to get 1 row got %d", rows)
	}
}
