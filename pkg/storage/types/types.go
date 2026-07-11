package types //nolint:revive

var SupportedIndexTypes = []string{
	IndexTypeTSDB,
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

const (
	StorageTypeAWS          = "aws"
	StorageTypeAlibabaCloud = "alibabacloud"
	StorageTypeAzure        = "azure"
	StorageTypeBOS          = "bos"
	StorageTypeCOS          = "cos"
	StorageTypeFileSystem   = "filesystem"
	StorageTypeGCS          = "gcs"
	StorageTypeS3           = "s3"
	StorageTypeSwift        = "swift"

	IndexTypeTSDB = "tsdb"
)
