package sarama

type (
	QuotaEntityType string

	QuotaMatchType int
)

// ref: https://github.com/apache/kafka/blob/trunk/clients/src/main/java/org/apache/kafka/common/quota/ClientQuotaEntity.java
const (
	QuotaEntityUser     QuotaEntityType = "user"
	QuotaEntityClientID QuotaEntityType = "client-id"
	QuotaEntityIP       QuotaEntityType = "ip"
)

// ref: https://github.com/apache/kafka/blob/trunk/clients/src/main/java/org/apache/kafka/common/requests/DescribeClientQuotasRequest.java
const (
	QuotaMatchExact QuotaMatchType = iota
	QuotaMatchDefault
	QuotaMatchAny
)
