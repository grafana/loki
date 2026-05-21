package types //nolint:revive

var SupportedIndexTypes = []string{
	IndexTypeTSDB,
}

var UnsupportedIndexTypes = []string{
	IndexTypeBoltDB,
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

var UnsupportedStorageTypes = []string{
	StorageTypeAWSDynamo,
	StorageTypeBigTable,
	StorageTypeBigTableHashed,
	StorageTypeBoltDB,
	StorageTypeCassandra,
	StorageTypeGCP,
	StorageTypeGCPColumnKey,
	StorageTypeGrpc,
}

const (
	StorageTypeAWS            = "aws"
	StorageTypeAWSDynamo      = "aws-dynamo"
	StorageTypeAlibabaCloud   = "alibabacloud"
	StorageTypeAzure          = "azure"
	StorageTypeBOS            = "bos"
	StorageTypeBigTable       = "bigtable"
	StorageTypeBigTableHashed = "bigtable-hashed"
	StorageTypeBoltDB         = "boltdb"
	StorageTypeCOS            = "cos"
	StorageTypeCassandra      = "cassandra"
	StorageTypeFileSystem     = "filesystem"
	StorageTypeGCP            = "gcp"
	StorageTypeGCPColumnKey   = "gcp-columnkey"
	StorageTypeGCS            = "gcs"
	StorageTypeGrpc           = "grpc-store"
	StorageTypeLocal          = "local"
	StorageTypeS3             = "s3"
	StorageTypeSwift          = "swift"

	IndexTypeBoltDB = "boltdb-shipper"
	IndexTypeTSDB   = "tsdb"
)
