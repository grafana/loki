import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RingResponse, RingType, RingTypes } from "@/types/ring";
import { useCluster } from "@/contexts/use-cluster";
import { getRingProxyPath, needsTokens } from "@/lib/ring-utils";

export const AVAILABLE_RINGS: Array<{ id: RingType; title: string }> = [
  { id: RingTypes.INGESTER, title: "Ingester" },
  { id: RingTypes.PARTITION_INGESTER, title: "Partition Ingester" },
  { id: RingTypes.DISTRIBUTOR, title: "Distributor" },
  { id: RingTypes.PATTERN_INGESTER, title: "Pattern Ingester" },
  { id: RingTypes.QUERY_SCHEDULER, title: "Scheduler" },
  { id: RingTypes.COMPACTOR, title: "Compactor" },
  { id: RingTypes.RULER, title: "Ruler" },
  { id: RingTypes.INDEX_GATEWAY, title: "Index Gateway" },
];

// Function to parse ownership from HTML response
function parseOwnershipFromHTML(html: string): Record<string, string> {
  const ownerships: Record<string, string> = {};
  try {
    // Extract instance rows from the table
    const tableRegex = /<tbody[^>]*>([\s\S]*?)<\/tbody>/;
    const tableMatch = html.match(tableRegex);
    if (!tableMatch) return ownerships;

    const rowRegex = /<tr[^>]*>([\s\S]*?)<\/tr>/g;
    const rows = Array.from(tableMatch[1].matchAll(rowRegex));

    for (const row of rows) {
      const cellRegex = /<td[^>]*>([\s\S]*?)<\/td>/g;
      const cells = Array.from(row[1].matchAll(cellRegex)).map((m) =>
        m[1].trim().replace(/&nbsp;/g, "")
      );

      if (cells.length >= 10) {
        const id = cells[0];
        const ownership = cells[9].endsWith("%") ? cells[9] : `${cells[9]}%`;
        ownerships[id] = ownership;
      }
    }
  } catch (err) {
    console.error("Error parsing ring HTML:", err);
  }
  return ownerships;
}

export interface UseRingOptions {
  ringName?: RingType;
  isPaused?: boolean;
}

export interface UseRingResult {
  ring: RingResponse | null;
  error: string;
  isLoading: boolean;
  fetchRing: () => Promise<void>;
  forgetInstances: (
    instanceIds: string[]
  ) => Promise<{ success: number; total: number }>;
  uniqueStates: string[];
  uniqueZones: string[];
  isTokenBased: boolean;
}

export function useRing({
  ringName,
  isPaused = false,
}: UseRingOptions): UseRingResult {
  const { cluster } = useCluster();
  const [ring, setRing] = useState<RingResponse | null>(null);
  const [error, setError] = useState<string>("");
  const [isLoading, setIsLoading] = useState(false);
  const abortControllerRef = useRef<AbortController | undefined>(undefined);

  const isTokenBased = useMemo(() => needsTokens(ringName), [ringName]);

  const ringProxyPath = useCallback(() => {
    return getRingProxyPath(cluster?.members, ringName ?? "");
  }, [cluster, ringName]);

  const fetchRing = useCallback(async () => {
    if (!ringName) {
      setError("Ring name is required");
      return;
    }
    const path = ringProxyPath();
    if (!path) {
      setError("No cluster members available");
      return;
    }

    // Cancel any in-flight requests
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    abortControllerRef.current = new AbortController();

    setIsLoading(true);
    try {
      // Fetch JSON data first
      const jsonResponse = await fetch(path, {
        headers: {
          Accept: "application/json",
        },
        signal: abortControllerRef.current.signal,
      });

      if (!jsonResponse.ok) {
        throw new Error(`Failed to fetch ring: ${jsonResponse.statusText}`);
      }

      const jsonData: RingResponse = await jsonResponse.json();
      if (!jsonData || !jsonData.shards) {
        setRing(null);
        return;
      }

      // Then fetch text/plain data to get ownership information
      const textResponse = await fetch(path, {
        headers: {
          Accept: "text/plain",
        },
        signal: abortControllerRef.current.signal,
      });

      if (!textResponse.ok) {
        throw new Error(
          `Failed to fetch ring ownership: ${textResponse.statusText}`
        );
      }

      const text = await textResponse.text();
      const ownerships = parseOwnershipFromHTML(text);

      // Merge ownership information into the JSON data
      const mergedData: RingResponse = {
        ...jsonData,
        shards: jsonData.shards.map((shard) => ({
          ...shard,
          ownership: ownerships[shard.id] || "0%",
        })),
      };

      setRing(mergedData);
      setError("");
    } catch (err) {
      if (err instanceof Error && err.name === "AbortError") {
        return;
      }
      console.error("Error fetching ring:", err);
      setError(err instanceof Error ? err.message : "Unknown error");
      setRing(null);
    } finally {
      setIsLoading(false);
    }
  }, [ringName, ringProxyPath]);

  const forgetInstances = useCallback(
    async (instanceIds: string[]) => {
      const path = ringProxyPath();
      if (!path) {
        throw new Error("Ring name and node name are required");
      }

      let success = 0;
      const total = instanceIds.length;

      for (const instanceId of instanceIds) {
        try {
          const formData = new FormData();
          formData.append("forget", instanceId);
          const response = await fetch(path, {
            method: "POST",
            body: formData,
          });
          if (response.ok) {
            success++;
          }
        } catch (err) {
          console.error(`Error forgetting instance ${instanceId}:`, err);
        }
      }

      return { success, total };
    },
    [ringProxyPath]
  );

  // Calculate instance statistics
  const { uniqueStates, uniqueZones } = useMemo(() => {
    if (!ring?.shards) {
      return { uniqueStates: [], uniqueZones: [] };
    }

    const states = new Set<string>();
    const zones = new Set<string>();
    const byState: Record<string, number> = {};

    ring.shards.forEach((instance) => {
      const state = instance.state || "unknown";
      byState[state] = (byState[state] || 0) + 1;
      if (state && state.trim()) {
        states.add(state);
      }
      if (instance.zone && instance.zone.trim()) {
        zones.add(instance.zone);
      }
    });

    return {
      uniqueStates: Array.from(states).sort(),
      uniqueZones: Array.from(zones).sort(),
    };
  }, [ring?.shards]);

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
    fetchRing();

    // Set up refresh interval only if not paused
    if (!isPaused) {
      const intervalId = setInterval(() => {
        fetchRing();
      }, 5000);

      // Cleanup interval on unmount
      return () => {
        clearInterval(intervalId);
      };
    }
  }, [fetchRing, isPaused]);

  return {
    ring,
    error,
    isLoading,
    fetchRing,
    forgetInstances,
    uniqueStates,
    uniqueZones,
    isTokenBased,
  };
}
