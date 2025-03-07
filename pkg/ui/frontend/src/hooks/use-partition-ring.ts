import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  PartitionInstance,
  PartitionRingResponse,
  RingTypes,
} from "@/types/ring";
import { useCluster } from "@/contexts/use-cluster";
import { useRateNodeMetrics } from "./use-rate-node-metrics";
import { getRingProxyPath, parseZoneFromOwner } from "@/lib/ring-utils";

interface PartitionState {
  partitions: PartitionInstance[];
  error: string;
  isLoading: boolean;
}

export interface UsePartitionRingResult {
  partitions: PartitionInstance[];
  error: string;
  isLoading: boolean;
  fetchPartitions: () => Promise<void>;
  changePartitionState: (
    partitionIds: number[],
    newState: string
  ) => Promise<{ success: number; total: number }>;
  partitionsByState: Record<string, number>;
  uniqueStates: string[];
  uniqueZones: string[];
}

// Map of partition states to their numeric values
const PARTITION_STATE_VALUES: Record<string, string> = {
  "0": "PartitionUnknown",
  "1": "PartitionPending",
  "2": "PartitionActive",
  "3": "PartitionInactive",
  "4": "PartitionDeleted",
};

export interface UsePartitionRingOptions {
  isPaused?: boolean;
}

export function usePartitionRing({
  isPaused = false,
}: UsePartitionRingOptions = {}): UsePartitionRingResult {
  const { cluster, isLoading: isClusterLoading } = useCluster();
  const [state, setState] = useState<PartitionState>({
    partitions: [],
    error: "",
    isLoading: false,
  });
  const abortControllerRef = useRef<AbortController | undefined>(undefined);

  const ringProxyPath = useCallback(() => {
    return getRingProxyPath(cluster?.members, RingTypes.PARTITION_INGESTER);
  }, [cluster]);

  const { fetchMetrics } = useRateNodeMetrics();
  const fetchPartitions = useCallback(async () => {
    if (!ringProxyPath()) {
      setState((prev) => ({
        ...prev,
        partitions: [],
        error: "No cluster members available",
        isLoading: false,
      }));
      return;
    }

    // Cancel any in-flight requests
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }

    // Create new abort controller for this request
    abortControllerRef.current = new AbortController();

    try {
      setState((prev) => ({ ...prev, isLoading: true, error: "" }));
      const response = await fetch(ringProxyPath(), {
        signal: abortControllerRef.current.signal,
        headers: {
          Accept: "application/json",
        },
      });

      if (!response.ok) {
        throw new Error(`Failed to fetch partitions: ${response.statusText}`);
      }

      const data: PartitionRingResponse = await response.json();
      // Denormalize partitions and add zone information
      const denormalizedPartitions = data.partitions.flatMap(
        (partition: PartitionInstance) =>
          partition.owner_ids.map((owner) => ({
            ...partition,
            owner_id: owner,
            owner_ids: [owner],
            zone: parseZoneFromOwner(owner),
          }))
      );

      const uniqueNodes = Array.from(
        new Set(
          denormalizedPartitions
            .map((p) => p.owner_ids)
            .flat()
            .filter((id) => id !== undefined)
        )
      );
      // Fetch metrics for all nodes
      const metricsData = await fetchMetrics({
        nodeNames: uniqueNodes,
        metrics: [
          "loki_ingest_storage_reader_fetch_bytes_total",
          "loki_ingest_storage_reader_fetch_compressed_bytes_total",
        ],
      });
      // Update partitions with metrics
      setState((prev) => ({
        ...prev,
        isLoading: false,
        partitions: denormalizedPartitions.map((partition) => {
          if (!partition.owner_id) return partition;
          const nodeRates = metricsData[partition.owner_id] || [];
          return {
            ...partition,
            uncompressedRate:
              nodeRates.find(
                (r) => r.name === "loki_ingest_storage_reader_fetch_bytes_total"
              )?.rate || 0,
            compressedRate:
              nodeRates.find(
                (r) =>
                  r.name ===
                  "loki_ingest_storage_reader_fetch_compressed_bytes_total"
              )?.rate || 0,
          };
        }),
      }));
    } catch (err) {
      // Only set error if it's not an abort error
      if (err instanceof Error && err.name !== "AbortError") {
        setState((prev) => ({
          ...prev,
          error: err instanceof Error ? err.message : "Unknown error occurred",
          isLoading: false,
        }));
      }
    }
  }, [ringProxyPath, fetchMetrics]);

  const changePartitionState = useCallback(
    async (selectedPartitionDetails: number[], newState: string) => {
      if (!ringProxyPath()) {
        throw new Error("No cluster members available");
      }
      const uniquePartitions = Array.from(new Set(selectedPartitionDetails));
      const total = uniquePartitions.length;
      let success = 0;

      await Promise.allSettled(
        uniquePartitions.map(async (partitionId) => {
          const formData = new FormData();
          formData.append("action", "change_state");
          formData.append("partition_id", partitionId.toString());
          const stateValue = PARTITION_STATE_VALUES[newState];
          if (stateValue === undefined) {
            throw new Error(`Invalid partition state: ${newState}`);
          }
          formData.append("partition_state", stateValue.toString());

          const response = await fetch(ringProxyPath(), {
            method: "POST",
            body: formData,
          });

          if (!response.ok) {
            const error = await response.text();
            throw new Error(
              `Failed to change state for partition ${partitionId}: ${error}`
            );
          }

          success++;
          return partitionId;
        })
      );

      return { success, total };
    },
    [ringProxyPath]
  );

  // Calculate partition statistics with memoization for each part
  const partitionStats = useMemo(() => {
    const states = new Set<string>();
    const zones = new Set<string>();
    const byState: Record<string, number> = {};

    state.partitions.forEach((partition) => {
      const partitionState = partition.state.toString();
      byState[partitionState] = (byState[partitionState] || 0) + 1;
      states.add(partitionState);

      partition.owner_ids.forEach((owner) => {
        const zone = owner.split("-")[2]; // Extract zone from owner ID
        if (zone) {
          zones.add(zone);
        }
      });
    });

    return {
      partitionsByState: byState,
      uniqueStates: Array.from(states).sort(),
      uniqueZones: Array.from(zones).sort(),
    };
  }, [state.partitions]);

  // Cleanup effect
  useEffect(() => {
    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, []);

  // Initial fetch and refresh loop
  useEffect(() => {
    fetchPartitions();

    if (!isPaused) {
      const intervalId = setInterval(fetchPartitions, 5000);
      return () => clearInterval(intervalId);
    }
  }, [fetchPartitions, isPaused]);

  return {
    partitions: state.partitions,
    error: state.error,
    isLoading: state.isLoading || isClusterLoading,
    fetchPartitions,
    changePartitionState,
    ...partitionStats,
  };
}
