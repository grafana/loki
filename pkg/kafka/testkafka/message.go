// SPDX-License-Identifier: AGPL-3.0-only

package testkafka

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// CreateProduceResponseError returns a kmsg.ProduceResponse containing err for the input topic and partition.
func CreateProduceResponseError(version int16, topic string, partition int32, err *kerr.Error) *kmsg.ProduceResponse {
	return &kmsg.ProduceResponse{
		Version: version,
		Topics: []kmsg.ProduceResponseTopic{
			{
				Topic: topic,
				Partitions: []kmsg.ProduceResponseTopicPartition{
					{
						Partition: partition,
						ErrorCode: err.Code,
					},
				},
			},
		},
	}
}
