package sarama

type apiVersionRange struct {
	minVersion int16
	maxVersion int16
}

type apiVersionMap map[int16]*apiVersionRange

// restrictApiVersion selects the appropriate API version for a given protocol body according to
// the client and broker version ranges. By default, it selects the maximum version supported by both
// client and broker, capped by the maximum Kafka version from Config.
// It then calls setVersion() on the protocol body.
// If no valid version is found, an error is returned.
func restrictApiVersion(pb protocolBody, brokerVersions apiVersionMap) error {
	key := pb.key()
	// Since message constructors take a Kafka version and select the maximum supported protocol version already, we can
	// rely on pb.version() being the max version supported for the user-selected Kafka API version.
	clientMax := pb.version()

	if brokerVersionRange := brokerVersions[key]; brokerVersionRange != nil {
		// Select the maximum version that both client and server support
		// Clamp to the client max to respect user preference above broker advertised version range
		pb.setVersion(min(clientMax, max(min(clientMax, brokerVersionRange.maxVersion), brokerVersionRange.minVersion)))
		return nil
	}

	return nil // no version ranges available, no restriction
}

const (
	apiKeyProduce                      = 0
	apiKeyFetch                        = 1
	apiKeyListOffsets                  = 2
	apiKeyMetadata                     = 3
	apiKeyLeaderAndIsr                 = 4
	apiKeyStopReplica                  = 5
	apiKeyUpdateMetadata               = 6
	apiKeyControlledShutdown           = 7
	apiKeyOffsetCommit                 = 8
	apiKeyOffsetFetch                  = 9
	apiKeyFindCoordinator              = 10
	apiKeyJoinGroup                    = 11
	apiKeyHeartbeat                    = 12
	apiKeyLeaveGroup                   = 13
	apiKeySyncGroup                    = 14
	apiKeyDescribeGroups               = 15
	apiKeyListGroups                   = 16
	apiKeySaslHandshake                = 17
	apiKeyApiVersions                  = 18
	apiKeyCreateTopics                 = 19
	apiKeyDeleteTopics                 = 20
	apiKeyDeleteRecords                = 21
	apiKeyInitProducerId               = 22
	apiKeyOffsetForLeaderEpoch         = 23
	apiKeyAddPartitionsToTxn           = 24
	apiKeyAddOffsetsToTxn              = 25
	apiKeyEndTxn                       = 26
	apiKeyWriteTxnMarkers              = 27
	apiKeyTxnOffsetCommit              = 28
	apiKeyDescribeAcls                 = 29
	apiKeyCreateAcls                   = 30
	apiKeyDeleteAcls                   = 31
	apiKeyDescribeConfigs              = 32
	apiKeyAlterConfigs                 = 33
	apiKeyAlterReplicaLogDirs          = 34
	apiKeyDescribeLogDirs              = 35
	apiKeySASLAuth                     = 36
	apiKeyCreatePartitions             = 37
	apiKeyCreateDelegationToken        = 38
	apiKeyRenewDelegationToken         = 39
	apiKeyExpireDelegationToken        = 40
	apiKeyDescribeDelegationToken      = 41
	apiKeyDeleteGroups                 = 42
	apiKeyElectLeaders                 = 43
	apiKeyIncrementalAlterConfigs      = 44
	apiKeyAlterPartitionReassignments  = 45
	apiKeyListPartitionReassignments   = 46
	apiKeyOffsetDelete                 = 47
	apiKeyDescribeClientQuotas         = 48
	apiKeyAlterClientQuotas            = 49
	apiKeyDescribeUserScramCredentials = 50
	apiKeyAlterUserScramCredentials    = 51
)
