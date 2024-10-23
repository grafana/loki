package ingester

import (
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/util"
)

// PreparePartitionDownscaleHandler prepares the ingester's partition downscaling. The partition owned by the
// ingester will switch to INACTIVE state (read-only).
//
// Following methods are supported:
//
//   - GET
//     Returns timestamp when partition was switched to INACTIVE state, or 0, if partition is not in INACTIVE state.
//
//   - POST
//     Switches the partition to INACTIVE state (if not yet), and returns the timestamp when the switch to
//     INACTIVE state happened.
//
//   - DELETE
//     Sets partition back from INACTIVE to ACTIVE state, and returns 0 signalling the partition is not in INACTIVE state
func (i *Ingester) PreparePartitionDownscaleHandler(w http.ResponseWriter, r *http.Request) {
	logger := log.With(i.logger, "partition", i.ingestPartitionID)

	// Don't allow callers to change the shutdown configuration while we're in the middle
	// of starting or shutting down.
	if i.State() != services.Running {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	if !i.cfg.KafkaIngestion.Enabled {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	switch r.Method {
	case http.MethodPost:
		// It's not allowed to prepare the downscale while in PENDING state. Why? Because if the downscale
		// will be later cancelled, we don't know if it was requested in PENDING or ACTIVE state, so we
		// don't know to which state reverting back. Given a partition is expected to stay in PENDING state
		// for a short period, we simply don't allow this case.
		state, _, err := i.partitionRingLifecycler.GetPartitionState(r.Context())
		if err != nil {
			level.Error(logger).Log("msg", "failed to check partition state in the ring", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if state == ring.PartitionPending {
			level.Warn(logger).Log("msg", "received a request to prepare partition for shutdown, but the request can't be satisfied because the partition is in PENDING state")
			w.WriteHeader(http.StatusConflict)
			return
		}

		if err := i.partitionRingLifecycler.ChangePartitionState(r.Context(), ring.PartitionInactive); err != nil {
			level.Error(logger).Log("msg", "failed to change partition state to inactive", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

	case http.MethodDelete:
		state, _, err := i.partitionRingLifecycler.GetPartitionState(r.Context())
		if err != nil {
			level.Error(logger).Log("msg", "failed to check partition state in the ring", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// If partition is inactive, make it active. We ignore other states Active and especially Pending.
		if state == ring.PartitionInactive {
			// We don't switch it back to PENDING state if there are not enough owners because we want to guarantee consistency
			// in the read path. If the partition is within the lookback period we need to guarantee that partition will be queried.
			// Moving back to PENDING will cause us loosing consistency, because PENDING partitions are not queried by design.
			// We could move back to PENDING if there are not enough owners and the partition moved to INACTIVE more than
			// "lookback period" ago, but since we delete inactive partitions with no owners that moved to inactive since longer
			// than "lookback period" ago, it looks to be an edge case not worth to address.
			if err := i.partitionRingLifecycler.ChangePartitionState(r.Context(), ring.PartitionActive); err != nil {
				level.Error(logger).Log("msg", "failed to change partition state to active", "err", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	state, stateTimestamp, err := i.partitionRingLifecycler.GetPartitionState(r.Context())
	if err != nil {
		level.Error(logger).Log("msg", "failed to check partition state in the ring", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if state == ring.PartitionInactive {
		util.WriteJSONResponse(w, map[string]any{"timestamp": stateTimestamp.Unix()})
	} else {
		util.WriteJSONResponse(w, map[string]any{"timestamp": 0})
	}
}
