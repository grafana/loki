package instance

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
)

// HostFilterLabelMatchers are the set of labels that will be used to match
// against an incoming target.
var HostFilterLabelMatchers = []string{
	// Consul
	"__meta_consul_node",

	// Dockerswarm
	"__meta_dockerswarm_node_id",
	"__meta_dockerswarm_node_hostname",
	"__meta_dockerswarm_node_address",

	// Kubernetes node labels. Labels for `role: service` are omitted as
	// service targets have labels merged with discovered pods.
	"__meta_kubernetes_pod_node_name",
	"__meta_kubernetes_node_name",

	// Generic (applied by host_filter_relabel_configs)
	"__host__",
}

// DiscoveredGroups is a set of groups found via service discovery.
type DiscoveredGroups = map[string][]*targetgroup.Group

// GroupChannel is a channel that provides discovered target groups.
type GroupChannel = <-chan DiscoveredGroups

// HostFilter acts as a MITM between the discovery manager and the
// scrape manager, filtering out discovered targets that are not
// running on the same node as the agent itself.
type HostFilter struct {
	ctx    context.Context
	cancel context.CancelFunc

	host string

	inputCh  GroupChannel
	outputCh chan map[string][]*targetgroup.Group

	relabelMut sync.Mutex
	relabels   []*relabel.Config
}

// NewHostFilter creates a new HostFilter.
func NewHostFilter(host string, relabels []*relabel.Config) *HostFilter {
	ctx, cancel := context.WithCancel(context.Background())
	f := &HostFilter{
		ctx:    ctx,
		cancel: cancel,

		host:     host,
		relabels: relabels,

		outputCh: make(chan map[string][]*targetgroup.Group),
	}
	return f
}

// PatchSD patches services discoveries to optimize performance for host
// filtering. The discovered targets will be pruned to as close to the set
// that HostFilter will output as possible.
func (f *HostFilter) PatchSD(scrapes []*config.ScrapeConfig) {
	for _, sc := range scrapes {
		for _, d := range sc.ServiceDiscoveryConfigs {
			switch d := d.(type) {
			case *kubernetes.SDConfig:
				if d.Role == kubernetes.RolePod {
					d.Selectors = []kubernetes.SelectorConfig{{
						Role:  kubernetes.RolePod,
						Field: fmt.Sprintf("spec.nodeName=%s", f.host),
					}}
				}
			}
		}
	}

}

// SetRelabels updates the relabeling rules used by the HostFilter.
func (f *HostFilter) SetRelabels(relabels []*relabel.Config) {
	f.relabelMut.Lock()
	defer f.relabelMut.Unlock()
	f.relabels = relabels
}

// Run starts the HostFilter. It only exits when the HostFilter is stopped.
// Run will continually read from syncCh and filter groups discovered down to
// targets that are colocated on the same node as the one the HostFilter is
// running in.
func (f *HostFilter) Run(syncCh GroupChannel) {
	f.inputCh = syncCh

	for {
		select {
		case <-f.ctx.Done():
			return
		case data := <-f.inputCh:
			f.relabelMut.Lock()
			relabels := f.relabels
			f.relabelMut.Unlock()

			f.outputCh <- FilterGroups(data, f.host, relabels)
		}
	}
}

// Stop stops the host filter from processing more target updates.
func (f *HostFilter) Stop() {
	f.cancel()
}

// SyncCh returns a read only channel used by all the clients to receive
// target updates.
func (f *HostFilter) SyncCh() GroupChannel {
	return f.outputCh
}

// FilterGroups takes a set of DiscoveredGroups as input and filters out
// any Target that is not running on the host machine provided by host.
//
// This is done by looking at HostFilterLabelMatchers and __address__.
//
// If the discovered address is localhost or 127.0.0.1, the group is never
// filtered out.
func FilterGroups(in DiscoveredGroups, host string, configs []*relabel.Config) DiscoveredGroups {
	out := make(DiscoveredGroups, len(in))

	for name, groups := range in {
		groupList := make([]*targetgroup.Group, 0, len(groups))

		for _, group := range groups {
			newGroup := &targetgroup.Group{
				Targets: make([]model.LabelSet, 0, len(group.Targets)),
				Labels:  group.Labels,
				Source:  group.Source,
			}

			for _, target := range group.Targets {
				allLabels := mergeSets(target, group.Labels)
				processedLabels := relabel.Process(toLabelSlice(allLabels), configs...)

				if !shouldFilterTarget(processedLabels, host) {
					newGroup.Targets = append(newGroup.Targets, target)
				}
			}

			groupList = append(groupList, newGroup)
		}

		out[name] = groupList
	}

	return out
}

// shouldFilterTarget returns true when the target labels (combined with the set of common
// labels) should be filtered out by FilterGroups.
func shouldFilterTarget(lbls labels.Labels, host string) bool {
	shouldFilterTargetByLabelValue := func(labelValue string) bool {
		if addr, _, err := net.SplitHostPort(labelValue); err == nil {
			labelValue = addr
		}

		// Special case: always allow localhost/127.0.0.1
		if labelValue == "localhost" || labelValue == "127.0.0.1" {
			return false
		}

		return labelValue != host
	}

	lset := labels.New(lbls...)
	addressLabel := lset.Get(model.AddressLabel)
	if addressLabel == "" {
		// No address label. This is invalid and will generate an error by the scrape
		// manager, so we'll pass it on for now.
		return false
	}

	// If the __address__ label matches, we can quit early.
	if !shouldFilterTargetByLabelValue(addressLabel) {
		return false
	}

	// Fall back to checking metalabels as long as their values are nonempty.
	for _, check := range HostFilterLabelMatchers {
		// If any of the checked labels match for not being filtered out, we can
		// return before checking any of the other matchers.
		if addr := lset.Get(check); addr != "" && !shouldFilterTargetByLabelValue(addr) {
			return false
		}
	}

	// Nothing matches, filter it out.
	return true
}

// mergeSets merges the sets of labels together. Earlier sets take priority for label names.
func mergeSets(sets ...model.LabelSet) model.LabelSet {
	sz := 0
	for _, set := range sets {
		sz += len(set)
	}
	result := make(model.LabelSet, sz)

	for _, set := range sets {
		for labelName, labelValue := range set {
			if _, exist := result[labelName]; exist {
				continue
			}
			result[labelName] = labelValue
		}
	}

	return result
}

func toLabelSlice(set model.LabelSet) labels.Labels {
	slice := make(labels.Labels, 0, len(set))
	for name, value := range set {
		slice = append(slice, labels.Label{Name: string(name), Value: string(value)})
	}
	return slice
}
