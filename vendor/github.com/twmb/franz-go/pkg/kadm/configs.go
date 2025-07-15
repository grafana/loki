package kadm

import (
	"context"
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ConfigSynonym is a fallback value for a config.
type ConfigSynonym struct {
	Key    string            // Key is the fallback config name.
	Value  *string           // Value is the fallback config value, if any (sensitive is elided).
	Source kmsg.ConfigSource // Source is where this config synonym is defined from.
}

// Config is a configuration for a resource (topic, broker)
type Config struct {
	Key       string            // Key is the config name.
	Value     *string           // Value is the config value, if any.
	Sensitive bool              // Sensitive is if this config is sensitive (if so, Value is nil).
	Source    kmsg.ConfigSource // Source is where this config is defined from.

	// Synonyms contains fallback key/value pairs for this same
	// configuration key in order or preference. That is, if a config entry
	// is both dynamically defined and has a default value as well, the top
	// level config will be the dynamic value, while the synonym will be
	// the default.
	Synonyms []ConfigSynonym
}

// MaybeValue returns the config's value if it is non-nil, otherwise an empty
// string.
func (c *Config) MaybeValue() string {
	if c.Value != nil {
		return *c.Value
	}
	return ""
}

// ResourceConfig contains the configuration values for a resource (topic,
// broker, broker logger).
type ResourceConfig struct {
	Name       string   // Name is the name of this resource.
	Configs    []Config // Configs are the configs for this topic.
	Err        error    // Err is any error preventing configs from loading (likely, an unknown topic).
	ErrMessage string   // ErrMessage a potential extra message describing any error.
}

// ResourceConfigs contains the configuration values for many resources.
type ResourceConfigs []ResourceConfig

// On calls fn for the response config if it exists, returning the config and
// the error returned from fn. If fn is nil, this simply returns the config.
//
// The fn is given a copy of the config. This function returns the copy as
// well; any modifications within fn are modifications on the returned copy.
//
// If the resource does not exist, this returns kerr.UnknownTopicOrPartition.
func (rs ResourceConfigs) On(name string, fn func(*ResourceConfig) error) (ResourceConfig, error) {
	for _, r := range rs {
		if r.Name == name {
			if fn == nil {
				return r, nil
			}
			return r, fn(&r)
		}
	}
	return ResourceConfig{}, kerr.UnknownTopicOrPartition
}

// DescribeTopicConfigs returns the configuration for the requested topics.
//
// This may return *ShardErrors.
func (cl *Client) DescribeTopicConfigs(
	ctx context.Context,
	topics ...string,
) (ResourceConfigs, error) {
	if len(topics) == 0 {
		return nil, nil
	}
	return cl.describeConfigs(ctx, kmsg.ConfigResourceTypeTopic, topics)
}

// DescribeBrokerConfigs returns configuration for the requested brokers. If no
// brokers are requested, a single request is issued and any broker in the
// cluster replies with the cluster-level dynamic config values.
//
// This may return *ShardErrors.
func (cl *Client) DescribeBrokerConfigs(
	ctx context.Context,
	brokers ...int32,
) (ResourceConfigs, error) {
	var names []string
	if len(brokers) == 0 {
		names = append(names, "")
	}
	for _, b := range brokers {
		names = append(names, strconv.Itoa(int(b)))
	}
	return cl.describeConfigs(ctx, kmsg.ConfigResourceTypeBroker, names)
}

func (cl *Client) describeConfigs(
	ctx context.Context,
	kind kmsg.ConfigResourceType,
	names []string,
) (ResourceConfigs, error) {
	req := kmsg.NewPtrDescribeConfigsRequest()
	req.IncludeSynonyms = true
	for _, name := range names {
		rr := kmsg.NewDescribeConfigsRequestResource()
		rr.ResourceName = name
		rr.ResourceType = kind
		req.Resources = append(req.Resources, rr)
	}
	shards := cl.cl.RequestSharded(ctx, req)

	var configs []ResourceConfig
	return configs, shardErrEach(req, shards, func(kr kmsg.Response) error {
		resp := kr.(*kmsg.DescribeConfigsResponse)
		for _, r := range resp.Resources {
			if err := maybeAuthErr(r.ErrorCode); err != nil {
				return err
			}
			rc := ResourceConfig{
				Name:       r.ResourceName,
				Err:        kerr.ErrorForCode(r.ErrorCode),
				ErrMessage: unptrStr(r.ErrorMessage),
			}
			for _, c := range r.Configs {
				rcv := Config{
					Key:       c.Name,
					Value:     c.Value,
					Sensitive: c.IsSensitive,
					Source:    c.Source,
				}
				for _, syn := range c.ConfigSynonyms {
					rcv.Synonyms = append(rcv.Synonyms, ConfigSynonym{
						Key:    syn.Name,
						Value:  syn.Value,
						Source: syn.Source,
					})
				}
				rc.Configs = append(rc.Configs, rcv)
			}
			configs = append(configs, rc) // we are not storing in a map, no existence-check possible
		}
		return nil
	})
}

// IncrementalOp is a typed int8 that is used for incrementally updating
// configuration keys for topics and brokers.
type IncrementalOp int8

const (
	// SetConfig is an incremental operation to set an individual config
	// key.
	SetConfig IncrementalOp = iota

	// DeleteConfig is an incremental operation to delete an individual
	// config key.
	DeleteConfig

	// AppendConfig is an incremental operation to append a value to a
	// config key that is a list type.
	AppendConfig

	// SubtractConfig is an incremental operation to remove a value from a
	// config key that is a list type.
	SubtractConfig
)

// AlterConfig is an individual key/value operation to perform when altering
// configs.
//
// This package includes a StringPtr function to aid in building config values.
type AlterConfig struct {
	Op    IncrementalOp // Op is the incremental alter operation to perform. This is ignored for State alter functions.
	Name  string        // Name is the name of the config to alter.
	Value *string       // Value is the value to use when altering, if any.
}

// AlteredConfigsResponse contains the response for an individual alteration.
type AlterConfigsResponse struct {
	Name       string // Name is the name of this resource (topic name or broker number).
	Err        error  // Err is non-nil if the config could not be altered.
	ErrMessage string // ErrMessage a potential extra message describing any error.
}

// AlterConfigsResponses contains responses for many alterations.
type AlterConfigsResponses []AlterConfigsResponse

// On calls fn for the response name if it exists, returning the response and
// the error returned from fn. If fn is nil, this simply returns the response.
//
// The fn is given a copy of the response. This function returns the copy as
// well; any modifications within fn are modifications on the returned copy.
//
// If the resource does not exist, this returns kerr.UnknownTopicOrPartition.
func (rs AlterConfigsResponses) On(name string, fn func(*AlterConfigsResponse) error) (AlterConfigsResponse, error) {
	for _, r := range rs {
		if r.Name == name {
			if fn == nil {
				return r, nil
			}
			return r, fn(&r)
		}
	}
	return AlterConfigsResponse{}, kerr.UnknownTopicOrPartition
}

// AlterTopicConfigs incrementally alters topic configuration values.
//
// This method requires talking to a cluster that supports
// IncrementalAlterConfigs (officially introduced in Kafka v2.3, but many
// broker reimplementations support this request even if they do not support
// all other requests from Kafka v2.3).
//
// If you want to alter the entire configs state using the older AlterConfigs
// request, use AlterTopicConfigsState.
//
// This may return *ShardErrors. You may consider checking
// ValidateAlterTopicConfigs before using this method.
func (cl *Client) AlterTopicConfigs(ctx context.Context, configs []AlterConfig, topics ...string) (AlterConfigsResponses, error) {
	return cl.alterConfigs(ctx, false, configs, kmsg.ConfigResourceTypeTopic, topics)
}

// ValidateAlterTopicConfigs validates an incremental alter config for the given
// topics.
//
// This returns exactly what AlterTopicConfigs returns, but does not actually
// alter configurations.
func (cl *Client) ValidateAlterTopicConfigs(ctx context.Context, configs []AlterConfig, topics ...string) (AlterConfigsResponses, error) {
	return cl.alterConfigs(ctx, true, configs, kmsg.ConfigResourceTypeTopic, topics)
}

// AlterBrokerConfigs incrementally alters broker configuration values. If
// brokers are specified, this updates each specific broker. If no brokers are
// specified, this updates whole-cluster broker configuration values.
//
// This method requires talking to a cluster that supports
// IncrementalAlterConfigs (officially introduced in Kafka v2.3, but many
// broker reimplementations support this request even if they do not support
// all other requests from Kafka v2.3).
//
// If you want to alter the entire configs state using the older AlterConfigs
// request, use AlterBrokerConfigsState.
//
// This may return *ShardErrors. You may consider checking
// ValidateAlterBrokerConfigs before using this method.
func (cl *Client) AlterBrokerConfigs(ctx context.Context, configs []AlterConfig, brokers ...int32) (AlterConfigsResponses, error) {
	var names []string
	if len(brokers) == 0 {
		names = append(names, "")
	}
	for _, broker := range brokers {
		names = append(names, strconv.Itoa(int(broker)))
	}
	return cl.alterConfigs(ctx, false, configs, kmsg.ConfigResourceTypeBroker, names)
}

// ValidateAlterBrokerConfigs validates an incremental alter config for the given
// brokers.
//
// This returns exactly what AlterBrokerConfigs returns, but does not actually
// alter configurations.
func (cl *Client) ValidateAlterBrokerConfigs(ctx context.Context, configs []AlterConfig, brokers ...int32) (AlterConfigsResponses, error) {
	var names []string
	if len(brokers) == 0 {
		names = append(names, "")
	}
	for _, broker := range brokers {
		names = append(names, strconv.Itoa(int(broker)))
	}
	return cl.alterConfigs(ctx, true, configs, kmsg.ConfigResourceTypeBroker, names)
}

func (cl *Client) alterConfigs(
	ctx context.Context,
	dry bool,
	configs []AlterConfig,
	kind kmsg.ConfigResourceType,
	names []string,
) (AlterConfigsResponses, error) {
	req := kmsg.NewPtrIncrementalAlterConfigsRequest()
	req.ValidateOnly = dry
	for _, name := range names {
		rr := kmsg.NewIncrementalAlterConfigsRequestResource()
		rr.ResourceType = kind
		rr.ResourceName = name
		for _, config := range configs {
			rc := kmsg.NewIncrementalAlterConfigsRequestResourceConfig()
			rc.Name = config.Name
			rc.Value = config.Value
			switch config.Op {
			case SetConfig:
				rc.Op = kmsg.IncrementalAlterConfigOpSet
			case DeleteConfig:
				rc.Op = kmsg.IncrementalAlterConfigOpDelete
			case AppendConfig:
				rc.Op = kmsg.IncrementalAlterConfigOpAppend
			case SubtractConfig:
				rc.Op = kmsg.IncrementalAlterConfigOpSubtract
			}
			rr.Configs = append(rr.Configs, rc)
		}
		req.Resources = append(req.Resources, rr)
	}

	shards := cl.cl.RequestSharded(ctx, req)

	var rs []AlterConfigsResponse
	return rs, shardErrEach(req, shards, func(kr kmsg.Response) error {
		resp := kr.(*kmsg.IncrementalAlterConfigsResponse)
		for _, r := range resp.Resources {
			rs = append(rs, AlterConfigsResponse{ // we are not storing in a map, no existence check possible
				Name:       r.ResourceName,
				Err:        kerr.ErrorForCode(r.ErrorCode),
				ErrMessage: unptrStr(r.ErrorMessage),
			})
		}
		return nil
	})
}

// AlterTopicConfigsState alters the full state of topic configurations.
// All prior configuration is lost.
//
// This may return *ShardErrors. You may consider checking
// ValidateAlterTopicConfigs before using this method.
func (cl *Client) AlterTopicConfigsState(ctx context.Context, configs []AlterConfig, topics ...string) (AlterConfigsResponses, error) {
	return cl.alterConfigsState(ctx, false, configs, kmsg.ConfigResourceTypeTopic, topics)
}

// ValidateAlterTopicConfigs validates an AlterTopicConfigsState for the given
// topics.
//
// This returns exactly what AlterTopicConfigsState returns, but does not
// actually alter configurations.
func (cl *Client) ValidateAlterTopicConfigsState(ctx context.Context, configs []AlterConfig, topics ...string) (AlterConfigsResponses, error) {
	return cl.alterConfigsState(ctx, true, configs, kmsg.ConfigResourceTypeTopic, topics)
}

// AlterBrokerConfigs alters the full state of broker configurations. If
// broker are specified, this updates each specific broker. If no brokers are
// specified, this updates whole-cluster broker configuration values.
// All prior configuration is lost.
//
// This may return *ShardErrors. You may consider checking
// ValidateAlterBrokerConfigs before using this method.
func (cl *Client) AlterBrokerConfigsState(ctx context.Context, configs []AlterConfig, brokers ...int32) (AlterConfigsResponses, error) {
	var names []string
	if len(brokers) == 0 {
		names = append(names, "")
	}
	for _, broker := range brokers {
		names = append(names, strconv.Itoa(int(broker)))
	}
	return cl.alterConfigsState(ctx, false, configs, kmsg.ConfigResourceTypeBroker, names)
}

// ValidateAlterBrokerConfigs validates an AlterBrokerconfigsState for the
// given brokers.
//
// This returns exactly what AlterBrokerConfigs returns, but does not actually
// alter configurations.
func (cl *Client) ValidateAlterBrokerConfigsState(ctx context.Context, configs []AlterConfig, brokers ...int32) (AlterConfigsResponses, error) {
	var names []string
	if len(brokers) == 0 {
		names = append(names, "")
	}
	for _, broker := range brokers {
		names = append(names, strconv.Itoa(int(broker)))
	}
	return cl.alterConfigsState(ctx, true, configs, kmsg.ConfigResourceTypeBroker, names)
}

func (cl *Client) alterConfigsState(
	ctx context.Context,
	dry bool,
	configs []AlterConfig,
	kind kmsg.ConfigResourceType,
	names []string,
) (AlterConfigsResponses, error) {
	req := kmsg.NewPtrAlterConfigsRequest()
	req.ValidateOnly = dry
	for _, name := range names {
		rr := kmsg.NewAlterConfigsRequestResource()
		rr.ResourceType = kind
		rr.ResourceName = name
		for _, config := range configs {
			rc := kmsg.NewAlterConfigsRequestResourceConfig()
			rc.Name = config.Name
			rc.Value = config.Value
			rr.Configs = append(rr.Configs, rc)
		}
		req.Resources = append(req.Resources, rr)
	}

	shards := cl.cl.RequestSharded(ctx, req)

	var rs []AlterConfigsResponse
	return rs, shardErrEach(req, shards, func(kr kmsg.Response) error {
		resp := kr.(*kmsg.AlterConfigsResponse)
		for _, r := range resp.Resources {
			rs = append(rs, AlterConfigsResponse{ // we are not storing in a map, no existence check possible
				Name:       r.ResourceName,
				Err:        kerr.ErrorForCode(r.ErrorCode),
				ErrMessage: unptrStr(r.ErrorMessage),
			})
		}
		return nil
	})
}
