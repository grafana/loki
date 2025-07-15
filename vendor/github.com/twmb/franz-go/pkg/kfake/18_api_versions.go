package kfake

import (
	"fmt"
	"sort"
	"sync"

	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(18, 0, 3) }

func (c *Cluster) handleApiVersions(kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.ApiVersionsRequest)
	resp := req.ResponseKind().(*kmsg.ApiVersionsResponse)

	if resp.Version > 3 {
		resp.Version = 0 // downgrades to 0 if the version is unknown
	}

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// If we are handling ApiVersions, our package is initialized and we
	// build our response once.
	apiVersionsOnce.Do(func() {
		for _, v := range apiVersionsKeys {
			apiVersionsSorted = append(apiVersionsSorted, v)
		}
		sort.Slice(apiVersionsSorted, func(i, j int) bool {
			return apiVersionsSorted[i].ApiKey < apiVersionsSorted[j].ApiKey
		})
	})
	resp.ApiKeys = apiVersionsSorted

	return resp, nil
}

// Called at the beginning of every request, this validates that the client
// is sending requests within version ranges we can handle.
func checkReqVersion(key, version int16) error {
	v, exists := apiVersionsKeys[key]
	if !exists {
		return fmt.Errorf("unsupported request key %d", key)
	}
	if version < v.MinVersion {
		return fmt.Errorf("%s version %d below min supported version %d", kmsg.NameForKey(key), version, v.MinVersion)
	}
	if version > v.MaxVersion {
		return fmt.Errorf("%s version %d above max supported version %d", kmsg.NameForKey(key), version, v.MaxVersion)
	}
	return nil
}

var (
	apiVersionsMu   sync.Mutex
	apiVersionsKeys = make(map[int16]kmsg.ApiVersionsResponseApiKey)

	apiVersionsOnce   sync.Once
	apiVersionsSorted []kmsg.ApiVersionsResponseApiKey
)

// Every request we implement calls regKey in an init function, allowing us to
// fully correctly build our ApiVersions response.
func regKey(key, min, max int16) {
	apiVersionsMu.Lock()
	defer apiVersionsMu.Unlock()

	if key < 0 || min < 0 || max < 0 || max < min {
		panic(fmt.Sprintf("invalid registration, key: %d, min: %d, max: %d", key, min, max))
	}
	if _, exists := apiVersionsKeys[key]; exists {
		panic(fmt.Sprintf("doubly registered key %d", key))
	}
	apiVersionsKeys[key] = kmsg.ApiVersionsResponseApiKey{
		ApiKey:     key,
		MinVersion: min,
		MaxVersion: max,
	}
}
