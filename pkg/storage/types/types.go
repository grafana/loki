package types //nolint:revive

var SupportedIndexTypes = []string{
	BoltDBShipperType,
	TSDBType,
}

var DeprecatedIndexTypes = []string{
	StorageTypeAWS,
	StorageTypeAWSDynamo,
	StorageTypeBoltDB,
	StorageTypeGrpc,
}

var UnsupportedIndexTypes = []string{
	StorageTypeBigTable,
	StorageTypeBigTableHashed,
	StorageTypeCassandra,
	StorageTypeGCP,
	StorageTypeGCPColumnKey,
}

var SupportedStorageTypes = []string{
	// local file system
	StorageTypeFileSystem,
	// remote object storages
	StorageTypeAWS,
	StorageTypeAlibabaCloud,
	StorageTypeAzure,
	StorageTypeBOS,
	StorageTypeCOS,
	StorageTypeGCS,
	StorageTypeS3,
	StorageTypeSwift,
	StorageTypeNoop,
}

var DeprecatedStorageTypes = []string{
	StorageTypeAWSDynamo,
	StorageTypeGrpc,
}

var UnsupportedStorageTypes = []string{
	StorageTypeBigTable,
	StorageTypeBigTableHashed,
	StorageTypeCassandra,
	StorageTypeGCP,
	StorageTypeGCPColumnKey,
}

var TestingStorageTypes = []string{
	StorageTypeInMemory,
}

const (
	StorageTypeAlibabaCloud   = "alibabacloud"
	StorageTypeAWS            = "aws"
	StorageTypeAWSDynamo      = "aws-dynamo"
	StorageTypeAzure          = "azure"
	StorageTypeBOS            = "bos"
	StorageTypeBoltDB         = "boltdb"
	StorageTypeCassandra      = "cassandra"
	StorageTypeInMemory       = "inmemory"
	StorageTypeBigTable       = "bigtable"
	StorageTypeBigTableHashed = "bigtable-hashed"
	StorageTypeFileSystem     = "filesystem"
	StorageTypeGCP            = "gcp"
	StorageTypeGCPColumnKey   = "gcp-columnkey"
	StorageTypeGCS            = "gcs"
	StorageTypeGrpc           = "grpc-store"
	StorageTypeLocal          = "local"
	StorageTypeS3             = "s3"
	StorageTypeSwift          = "swift"
	StorageTypeCOS            = "cos"
	StorageTypeNoop           = "noop"

	BoltDBShipperType = "boltdb-shipper"
	TSDBType          = "tsdb"
)
