package kafka

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type mockKafkaClient struct {
	mu     sync.Mutex
	topics []string
	err    error
}

func (m *mockKafkaClient) RefreshMetadata(_ ...string) error {
	return nil
}

func (m *mockKafkaClient) Topics() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.topics, m.err
}

func Test_NewTopicManager(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		in          []string
		expectedErr bool
	}{
		{
			[]string{""},
			true,
		},
		{
			[]string{"^("},
			true,
		},
		{
			[]string{"foo"},
			false,
		},
		{
			[]string{"foo", "^foo.*"},
			false,
		},
	} {
		tt := tt
		t.Run(strings.Join(tt.in, ","), func(t *testing.T) {
			t.Parallel()
			_, err := newTopicManager(&mockKafkaClient{}, tt.in)
			if tt.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_Topics(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		manager     *topicManager
		expected    []string
		expectedErr bool
	}{
		{
			mustNewTopicsManager(&mockKafkaClient{err: errors.New("")}, []string{"foo"}),
			[]string{},
			true,
		},
		{
			mustNewTopicsManager(&mockKafkaClient{topics: []string{"foo", "foobar", "buzz"}}, []string{"^foo"}),
			[]string{"foo", "foobar"},
			false,
		},
		{
			mustNewTopicsManager(&mockKafkaClient{topics: []string{"foo", "foobar", "buzz"}}, []string{"^foo.*", "buzz"}),
			[]string{"buzz", "foo", "foobar"},
			false,
		},
	} {
		tt := tt
		t.Run("", func(t *testing.T) {
			t.Parallel()

			actual, err := tt.manager.Topics()
			if tt.expectedErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}

func mustNewTopicsManager(client topicClient, topics []string) *topicManager {
	t, err := newTopicManager(client, topics)
	if err != nil {
		panic(err)
	}
	return t
}
