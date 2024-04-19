package types

var SupportedIndexTypes = []string{
	BoltDBShipperType,
	TSDBType,
}

var DeprecatedIndexTypes = []string{
	StorageTypeAWS,
	StorageTypeAWSDynamo,
	StorageTypeBigTable,
	StorageTypeBigTableHashed,
	StorageTypeBoltDB,
	StorageTypeCassandra,
	StorageTypeGCP,
	StorageTypeGCPColumnKey,
	StorageTypeGrpc,
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
}

var DeprecatedStorageTypes = []string{
	StorageTypeAWSDynamo,
	StorageTypeBigTable,
	StorageTypeBigTableHashed,
	StorageTypeCassandra,
	StorageTypeGCP,
	StorageTypeGCPColumnKey,
	StorageTypeGrpc,
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

	BoltDBShipperType = "boltdb-shipper"
	TSDBType          = "tsdb"
)
