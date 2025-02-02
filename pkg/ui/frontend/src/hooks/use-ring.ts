import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RingResponse, RingInstance, RingType } from "@/types/ring";
import { useCluster } from "@/contexts/use-cluster";

export const AVAILABLE_RINGS: Array<{ id: RingType; title: string }> = [
  { id: "ingester", title: "Ingester" },
  { id: "partition-ingester", title: "Partition Ingester" },
  { id: "distributor", title: "Distributor" },
  { id: "pattern-ingester", title: "Pattern Ingester" },
  { id: "scheduler", title: "Scheduler" },
  { id: "compactor", title: "Compactor" },
  { id: "ruler", title: "Ruler" },
  { id: "index-gateway", title: "Index Gateway" },
];

// Function to determine if a ring type needs tokens
function needsTokens(ringName: RingType): boolean {
  return [
    "ingester",
    "partition-ingester",
    "index-gateway",
    "ruler",
    "pattern-ingester",
  ].includes(ringName);
}

// Utility function to get ring proxy path
function getRingProxyPath(
  nodeName: string,
  ringName: RingType,
  withTokens = false
): string {
  const basePath = `/ui/api/v1/proxy/${nodeName}`;
  const ringPath =
    ringName === "partition-ingester"
      ? "/partition-ring"
      : ringName === "ingester"
      ? "/ring"
      : `/${ringName}/ring`;
  const tokensParam = withTokens ? "?tokens=true" : "";
  return `${basePath}${ringPath}${tokensParam}`;
}

// Function to parse tokens and ownership from HTML response
function parseTokensFromHTML(
  html: string,
  instanceId: string
): { tokens: number[]; ownership: string } | null {
  try {
    // Find the instance's section in the HTML
    const instanceSection = html
      .split("<h2>Instance: ")
      .find((section) => section.startsWith(instanceId));
    if (!instanceSection) return null;

    // Extract tokens for this specific instance
    const tokensMatch = instanceSection.match(/Tokens:<br\/>\s+([\d\s]+)/);
    if (!tokensMatch) return null;

    // Split and parse tokens
    const tokens = tokensMatch[1]
      .trim()
      .split(/\s+/)
      .map((t) => parseInt(t, 10))
      .filter((t) => !isNaN(t));

    // Find the instance's row in the table to get ownership
    const instanceRowRegex = new RegExp(
      `<tr[^>]*>\\s*<td>${instanceId}</td>([^]*?)</tr>`,
      "s"
    );
    const instanceRow = html.match(instanceRowRegex);

    let ownership = "0%";
    if (instanceRow) {
      // Look for the ownership cell which is the second to last <td> before the actions
      const ownershipMatch = instanceRow[1].match(/<td>(\d+)%<\/td>/);
      if (ownershipMatch) {
        ownership = ownershipMatch[1] + "%";
      }
    }

    return { tokens, ownership };
  } catch (err) {
    console.error(
      "Error parsing tokens from HTML for instance",
      instanceId,
      err
    );
    return null;
  }
}

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

// Function to parse ring data from HTML response
function parseRingFromHTML(html: string): RingResponse {
  let now = new Date().toISOString();
  const shards: RingInstance[] = [];

  try {
    // Parse current time from the HTML
    const currentTimeMatch = html.match(/Current time: ([^<]+)/);
    if (currentTimeMatch) {
      // Parse the Go time format "2006-01-02 15:04:05.999999999 -0700 MST"
      const timeStr = currentTimeMatch[1].split(" m=")[0]; // Remove the monotonic clock part
      try {
        now = new Date(timeStr).toISOString();
      } catch (err) {
        console.error("Error parsing current time:", err);
      }
    }

    // Extract instance rows from the table
    const tableRegex = /<tbody[^>]*>([\s\S]*?)<\/tbody>/;
    const tableMatch = html.match(tableRegex);
    if (!tableMatch) return { now, shards };

    const rowRegex = /<tr[^>]*>([\s\S]*?)<\/tr>/g;
    const rows = Array.from(tableMatch[1].matchAll(rowRegex));

    for (const row of rows) {
      const cellRegex = /<td[^>]*>([\s\S]*?)<\/td>/g;
      const cells = Array.from(row[1].matchAll(cellRegex)).map((m) =>
        m[1].trim().replace(/&nbsp;/g, "")
      );

      if (cells.length < 8) continue;

      const id = cells[0];
      const zone = cells[1];
      const state = cells[2];
      const address = cells[3];
      const registeredAt = cells[4];
      const readOnly = cells[5] === "true";
      const readOnlyUpdated = cells[6];
      const lastHeartbeat = cells[7];

      // Parse the registered timestamp
      let registeredTimestamp = registeredAt;
      try {
        // Handle ISO format
        registeredTimestamp = new Date(registeredAt).toISOString();
      } catch (err) {
        console.error("Error parsing registered timestamp:", err);
      }

      // Parse the read-only updated timestamp
      let readOnlyUpdatedTimestamp = readOnlyUpdated;
      if (readOnlyUpdated) {
        try {
          readOnlyUpdatedTimestamp = new Date(readOnlyUpdated).toISOString();
        } catch (err) {
          console.error("Error parsing read-only updated timestamp:", err);
        }
      }

      // Parse the last heartbeat timestamp
      let timestamp = now;
      if (lastHeartbeat) {
        const timeAgoMatch = lastHeartbeat.match(/(\d+)([smh]) ago/);
        if (timeAgoMatch) {
          const value = parseInt(timeAgoMatch[1], 10);
          const unit = timeAgoMatch[2];
          const msAgo =
            value * (unit === "h" ? 3600000 : unit === "m" ? 60000 : 1000);
          timestamp = new Date(Date.now() - msAgo).toISOString();
        } else {
          try {
            timestamp = new Date(lastHeartbeat).toISOString();
          } catch (err) {
            console.error("Error parsing last heartbeat timestamp:", err);
          }
        }
      }

      const instance: RingInstance = {
        id,
        state,
        address,
        timestamp,
        zone,
        registered_timestamp: registeredTimestamp,
        read_only: readOnly,
        read_only_updated_timestamp:
          readOnlyUpdatedTimestamp || registeredTimestamp,
        tokens: [],
        ownership: "0%",
      };

      // Parse tokens and ownership if available
      const tokenInfo = parseTokensFromHTML(html, id);
      if (tokenInfo) {
        instance.tokens = tokenInfo.tokens;
        instance.ownership = tokenInfo.ownership;
      } else {
        // Try to get tokens and ownership from the table cells
        if (cells.length >= 10) {
          const tokenCount = parseInt(cells[8], 10);
          if (!isNaN(tokenCount)) {
            // We don't have actual tokens in the table row, but we know how many there are
            instance.tokens = new Array(tokenCount).fill(0);
          }
          if (cells[9]) {
            instance.ownership = cells[9].endsWith("%")
              ? cells[9]
              : `${cells[9]}%`;
          }
        }
      }

      shards.push(instance);
    }
  } catch (err) {
    console.error("Error parsing ring HTML:", err);
  }

  return { now, shards };
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
  const abortControllerRef = useRef<AbortController>();

  const firstNodeName = useMemo(() => {
    if (!cluster?.members) return null;
    return Object.keys(cluster.members)[0] || null;
  }, [cluster]);

  const isTokenBased = useMemo(
    () => (ringName ? needsTokens(ringName) : false),
    [ringName]
  );

  const fetchRing = useCallback(async () => {
    if (!ringName) {
      setError("Ring name is required");
      return;
    }

    if (!firstNodeName) {
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
      const jsonResponse = await fetch(
        getRingProxyPath(firstNodeName, ringName, isTokenBased),
        {
          headers: {
            Accept: "application/json",
          },
          signal: abortControllerRef.current.signal,
        }
      );

      if (!jsonResponse.ok) {
        throw new Error(`Failed to fetch ring: ${jsonResponse.statusText}`);
      }

      const jsonData: RingResponse = await jsonResponse.json();
      if (!jsonData || !jsonData.shards) {
        setRing(null);
        return;
      }

      // Then fetch text/plain data to get ownership information
      const textResponse = await fetch(
        getRingProxyPath(firstNodeName, ringName, false),
        {
          headers: {
            Accept: "text/plain",
          },
          signal: abortControllerRef.current.signal,
        }
      );

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
  }, [firstNodeName, ringName, isTokenBased]);

  const forgetInstances = useCallback(
    async (instanceIds: string[]) => {
      if (!ringName || !firstNodeName) {
        throw new Error("Ring name and node name are required");
      }

      let success = 0;
      const total = instanceIds.length;

      for (const instanceId of instanceIds) {
        try {
          const formData = new FormData();
          formData.append("forget", instanceId);
          const response = await fetch(
            `${getRingProxyPath(firstNodeName, ringName)}`,
            {
              method: "POST",
              body: formData,
            }
          );
          if (response.ok) {
            success++;
          }
        } catch (err) {
          console.error(`Error forgetting instance ${instanceId}:`, err);
        }
      }

      return { success, total };
    },
    [ringName, firstNodeName]
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
