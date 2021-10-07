package util

import (
	"context"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/services"
)

const (
	RingKeyOfLeader = 0
)

type ringWatcher struct {
	log           log.Logger
	ring          ring.ReadRing
	notifications util.DNSNotifications
	lookupPeriod  time.Duration
	addresses     []string
}

// NewRingWatcher creates a new Ring watcher and returns a service that is wrapping it.
func NewRingWatcher(log log.Logger, ring ring.ReadRing, lookupPeriod time.Duration, notifications util.DNSNotifications) (services.Service, error) {
	w := &ringWatcher{
		log:           log,
		ring:          ring,
		notifications: notifications,
		lookupPeriod:  lookupPeriod,
	}
	return services.NewBasicService(nil, w.watchLoop, nil), nil
}

// watchLoop watches for changes in DNS and sends notifications.
func (w *ringWatcher) watchLoop(servCtx context.Context) error {

	syncTicker := time.NewTicker(w.lookupPeriod)
	defer syncTicker.Stop()

	for {
		select {
		case <-servCtx.Done():
			return nil
		case <-syncTicker.C:
			w.lookupAddresses()
		}
	}
}

func (w *ringWatcher) lookupAddresses() {

	addrs, err := w.getAddresses()
	if err != nil {
		level.Error(w.log).Log("msg", "error getting addresses from ring", "err", err)
	}

	if len(addrs) == 0 {
		return
	}
	toAdd := make([]string, 0, len(addrs))
	for i, newAddr := range addrs {
		level.Debug(w.log).Log("msg", "found scheduler", "addr", newAddr)
		alreadyExists := false
		for _, currAddr := range w.addresses {
			if currAddr == newAddr {
				alreadyExists = true
			}
		}
		if !alreadyExists {
			toAdd = append(toAdd, addrs[i])
		}
	}
	toRemove := make([]string, 0, len(w.addresses))
	for i, existingAddr := range w.addresses {
		stillExists := false
		for _, newAddr := range addrs {
			if newAddr == existingAddr {
				stillExists = true
			}
		}
		if !stillExists {
			toRemove = append(toRemove, w.addresses[i])
		}
	}

	for _, ta := range toAdd {
		level.Debug(w.log).Log("msg", fmt.Sprintf("adding connection to scheduler at address: %s", ta))
		w.notifications.AddressAdded(ta)
	}

	for _, tr := range toRemove {
		level.Debug(w.log).Log("msg", fmt.Sprintf("removing connection to scheduler at address: %s", tr))
		w.notifications.AddressRemoved(tr)
	}

	w.addresses = addrs

}

func (w *ringWatcher) getAddresses() ([]string, error) {
	var addrs []string

	// If there are less than 2 existing addresses, odds are we are running just a single instance
	// so just get the first healthy address and use it. If the call returns to continue on to
	// check for the actual replicaset instances
	if len(w.addresses) < 2 {
		rs, err := w.ring.GetAllHealthy(ring.WriteNoExtend)
		if err != nil {
			return nil, err
		}
		addrs = rs.GetAddresses()
		if len(addrs) == 1 {
			return addrs, nil
		}
	}

	bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
	rs, err := w.ring.Get(RingKeyOfLeader, ring.WriteNoExtend, bufDescs, bufHosts, bufZones)
	if err != nil {
		return nil, err
	}

	return rs.GetAddresses(), nil
}
