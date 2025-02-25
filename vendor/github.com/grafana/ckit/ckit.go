// Package ckit is a cluster toolkit for creating distributed systems. Nodes
// use gossip over HTTP/2 to maintain a list of all Nodes registered in the
// cluster.
//
// Nodes can optionally synchronize their state with a Sharder, which is used
// to perform consistent hashing and shard ownership of keys across the
// cluster.
package ckit
