/*
Package client provides high-level abstractions for configuring Kafka clients,
synchronous and asynchronous mechanisms to commit offsets for both direct
and group consumers, and fetching offset metadata from Kafka brokers.

To create a Kafka client, use the [NewClient] function. [ConsumerOpts] and
[ProducerOpts] returns a set of options that are common for consumer and
producer clients.

	// Create a Kafka client to consume records.
	client, err := NewClient(cfg, logger, reg, ConsumerOpts(cfg)...)
	...

	// Create a Kafka client to produce records.
	client, err := NewClient(cfg, logger, reg, ProducerOpts(cfg)...)
	...
	// Create a Kafka client to consume and produce records. This requires
	// the broker supports both consumer and producer APIs.
	var opts []kgo.Opt
	opts = append(opts, ConsumerOpts(cfg)...)
	opts = append(opts, ProducerOpts(cfg)...)
	client, err := NewClient(cfg, logger, reg, opts...)
	...

To commit offsets use either a [Committer], [GroupCommitter], [AsyncCommitter]
or [AsyncGroupCommitter]. A [Committer], and its equivalent async
implementation [AsyncCommitter] allows committing offsets for any topic,
partition and consumer group; while a [GroupCommitter] its equivalent async
implementation [AsyncGroupCommitter] allows committing offsets for any
partitions within a single topic and consumer group.

	// Commit offsets.
	committer := NewCommitter(kadm.NewClient(client))
	err := committer.Commit(ctx, topic, partition, consumerGroup, offset)
	...

	groupCommitter := NewGroupCommitter(kadm.NewClient(client), topic, consumerGroup)
	err := groupCommitter.Commit(ctx, offset)
	...

	// Commit offsets async.
	asyncCommitter := NewAsyncCommitter(kadm.NewClient(client))
	// Can return err if context is canceled.
	err := asyncCommitter.Commit(ctx, topic, partition, consumerGroup, offset)
	...

	asyncGroupCommitter := NewAsyncGroupCommitter(kadm.NewClient(client), topic, consumerGroup)
	err := asyncGroupCommitter.Commit(ctx, offset)
	...

	// Commit offsets async.
*/
package client
