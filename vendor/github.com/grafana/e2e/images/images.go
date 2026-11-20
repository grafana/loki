// Package images provides Docker image version constants for e2e test services.
package images

var (
	// These are variables so that they can be modified.
	Memcached = "memcached:1.6.12"
	Redis     = "redis:7.0.7"
	// MinIO archived its community edition: pre-built images stopped in Oct 2025, and the
	// Docker Hub AND quay.io community repos (minio/minio) were deleted in Sep 2026, so
	// neither registry resolves it anymore. pgsty/silo is a community MinIO fork that keeps
	// the same S3 API, MINIO_* env vars (including the legacy MINIO_ACCESS_KEY/
	// MINIO_SECRET_KEY that db.NewMinio sets, verified still honoured) and /minio/* routes;
	// only the server binary name changes from `minio` to `silo` (see db.NewMinio below).
	Minio = "pgsty/silo:RELEASE.2026-09-03T13-18-01Z"
	// RustFS is an Apache-2.0 S3-compatible server, used by db.NewRustFS as an alternative to MinIO.
	RustFS = "rustfs/rustfs:1.0.0@sha256:8cc9801755448b71a786705ce76692c77e14936cccd87cf2fc31842e58f4d1ff"
	// quay.io/minio/kes (the standalone KES key-encryption-service, used by NewKES/
	// NewMinioWithKES) is a separate image from MinIO/Silo itself and is still resolving on
	// quay.io as of Sep 2026, so it is left unchanged here.
	KES              = "quay.io/minio/kes:v0.17.1"
	Consul           = "consul:1.8.15"
	ETCD             = "gcr.io/etcd-development/etcd:v3.4.13"
	DynamoDB         = "amazon/dynamodb-local:1.17.0"
	BigtableEmulator = "shopify/bigtable-emulator:0.1.0"
	Cassandra        = "rinscy/cassandra:3.11.0"
	SwiftEmulator    = "bouncestorage/swift-aio:55ba4331"
	Kafka            = "apache/kafka:4.0.0"
	Dex              = "dexidp/dex:v2.41.1"
)
