package ring

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"time"

	"golang.org/x/exp/slices"
)

//go:embed partition_ring_status.gohtml
var partitionRingPageContent string
var partitionRingPageTemplate = template.Must(template.New("webpage").Funcs(template.FuncMap{
	"mod": func(i, j int32) bool {
		return i%j == 0
	},
	"formatTimestamp": func(ts time.Time) string {
		return ts.Format("2006-01-02 15:04:05 MST")
	},
}).Parse(partitionRingPageContent))

type PartitionRingUpdater interface {
	ChangePartitionState(ctx context.Context, partitionID int32, toState PartitionState) error
}

type PartitionRingPageHandler struct {
	reader  PartitionRingReader
	updater PartitionRingUpdater
}

func NewPartitionRingPageHandler(reader PartitionRingReader, updater PartitionRingUpdater) *PartitionRingPageHandler {
	return &PartitionRingPageHandler{
		reader:  reader,
		updater: updater,
	}
}

func (h *PartitionRingPageHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		h.handleGetRequest(w, req)
	case http.MethodPost:
		h.handlePostRequest(w, req)
	default:
		http.Error(w, "Unsupported HTTP method", http.StatusMethodNotAllowed)
	}
}

func (h *PartitionRingPageHandler) handleGetRequest(w http.ResponseWriter, req *http.Request) {
	var (
		ring     = h.reader.PartitionRing()
		ringDesc = ring.desc
	)

	// Prepare the data to render partitions in the page.
	partitionsByID := make(map[int32]partitionPageData, len(ringDesc.Partitions))
	for id, partition := range ringDesc.Partitions {
		owners := ring.PartitionOwnerIDsCopy(id)
		slices.Sort(owners)

		partitionsByID[id] = partitionPageData{
			ID:             id,
			Corrupted:      false,
			State:          partition.State,
			StateTimestamp: partition.GetStateTime(),
			OwnerIDs:       owners,
		}
	}

	// Look for owners of non-existing partitions. We want to provide visibility for such case
	// and we report the partition in corrupted state.
	for ownerID, owner := range ringDesc.Owners {
		partition, exists := partitionsByID[owner.OwnedPartition]

		if !exists {
			partition = partitionPageData{
				ID:             owner.OwnedPartition,
				Corrupted:      true,
				State:          PartitionUnknown,
				StateTimestamp: time.Time{},
				OwnerIDs:       []string{ownerID},
			}

			partitionsByID[owner.OwnedPartition] = partition
		}

		if !slices.Contains(partition.OwnerIDs, ownerID) {
			partition.OwnerIDs = append(partition.OwnerIDs, ownerID)
			partitionsByID[owner.OwnedPartition] = partition
		}
	}

	// Covert partitions to a list and sort it by ID.
	partitions := make([]partitionPageData, 0, len(partitionsByID))

	for _, partition := range partitionsByID {
		partitions = append(partitions, partition)
	}

	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].ID < partitions[j].ID
	})

	renderHTTPResponse(w, partitionRingPageData{
		Partitions: partitions,
		PartitionStateChanges: map[PartitionState]PartitionState{
			PartitionPending:  PartitionActive,
			PartitionActive:   PartitionInactive,
			PartitionInactive: PartitionActive,
		},
	}, partitionRingPageTemplate, req)
}

func (h *PartitionRingPageHandler) handlePostRequest(w http.ResponseWriter, req *http.Request) {
	if req.FormValue("action") == "change_state" {
		partitionID, err := strconv.Atoi(req.FormValue("partition_id"))
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid partition ID: %s", err.Error()), http.StatusBadRequest)
			return
		}

		toState, ok := PartitionState_value[req.FormValue("partition_state")]
		if !ok {
			http.Error(w, "invalid partition state", http.StatusBadRequest)
			return
		}

		if err := h.updater.ChangePartitionState(req.Context(), int32(partitionID), PartitionState(toState)); err != nil {
			http.Error(w, fmt.Sprintf("failed to change partition state: %s", err.Error()), http.StatusBadRequest)
			return
		}
	}

	// Implement PRG pattern to prevent double-POST and work with CSRF middleware.
	// https://en.wikipedia.org/wiki/Post/Redirect/Get
	w.Header().Set("Location", "#")
	w.WriteHeader(http.StatusFound)
}

type partitionRingPageData struct {
	Partitions []partitionPageData `json:"partitions"`

	// PartitionStateChanges maps the allowed state changes through the UI.
	PartitionStateChanges map[PartitionState]PartitionState `json:"-"`
}

type partitionPageData struct {
	ID             int32          `json:"id"`
	Corrupted      bool           `json:"corrupted"`
	State          PartitionState `json:"state"`
	StateTimestamp time.Time      `json:"state_timestamp"`
	OwnerIDs       []string       `json:"owner_ids"`
}
