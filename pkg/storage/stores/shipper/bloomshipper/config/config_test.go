package config

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBlocksCacheConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		desc string
		cfg  BlocksCacheConfig
		err  error
	}{
		{
			desc: "ttl not set",
			cfg: BlocksCacheConfig{
				SoftLimit: 1,
				HardLimit: 2,
			},
			err: errors.New("blocks cache ttl must not be 0"),
		},
		{
			desc: "soft limit not set",
			cfg: BlocksCacheConfig{
				TTL:       1,
				HardLimit: 2,
			},
			err: errors.New("blocks cache soft_limit must not be 0"),
		},
		{
			desc: "hard limit not set",
			cfg: BlocksCacheConfig{
				TTL:       1,
				SoftLimit: 1,
			},
			err: errors.New("blocks cache soft_limit must not be greater than hard_limit"),
		},
		{
			desc: "soft limit greater than hard limit",
			cfg: BlocksCacheConfig{
				TTL:       1,
				SoftLimit: 2,
				HardLimit: 1,
			},
			err: errors.New("blocks cache soft_limit must not be greater than hard_limit"),
		},
		{
			desc: "all good",
			cfg: BlocksCacheConfig{
				TTL:       1,
				SoftLimit: 1,
				HardLimit: 2,
			},
			err: nil,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.err != nil {
				require.ErrorContains(t, err, tc.err.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
