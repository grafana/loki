// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package configstore

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
)

// GetCodec returns the codec for encoding and decoding instance.Configs
// in the Remote store.
func GetCodec() codec.Codec {
	return &yamlCodec{}
}

type yamlCodec struct{}

func (*yamlCodec) Decode(bb []byte) (interface{}, error) {
	// Decode is called by kv.Clients with an empty slice when a
	// key is deleted. We should stop early here and don't return
	// an error so the deletion event propagates to watchers.
	if len(bb) == 0 {
		return nil, nil
	}

	r, err := gzip.NewReader(bytes.NewReader(bb))
	if err != nil {
		return nil, err
	}

	var sb strings.Builder
	if _, err := io.Copy(&sb, r); err != nil {
		return nil, err
	}
	return sb.String(), nil
}

func (*yamlCodec) Encode(v interface{}) ([]byte, error) {
	var buf bytes.Buffer

	var cfg string

	switch v := v.(type) {
	case string:
		cfg = v
	default:
		panic(fmt.Sprintf("unexpected type %T passed to yamlCodec.Encode", v))
	}

	w := gzip.NewWriter(&buf)

	if _, err := io.Copy(w, strings.NewReader(cfg)); err != nil {
		return nil, err
	}

	w.Close()
	return buf.Bytes(), nil
}

func (*yamlCodec) CodecID() string {
	return "agentConfig/yaml"
}
