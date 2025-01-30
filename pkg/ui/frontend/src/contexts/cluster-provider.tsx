import {
  useContext,
  useState,
  useCallback,
  useRef,
  useEffect,
  ReactNode,
} from "react";
import { Cluster } from "@/types/cluster";
import { ClusterContextValue } from "./types";
import { ClusterContext } from "./cluster-context";

interface ClusterProviderProps {
  children: ReactNode;
}

export function ClusterProvider({ children }: ClusterProviderProps) {
  const [cluster, setCluster] = useState<Cluster | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const isFetchingRef = useRef(false);

  const fetchCluster = useCallback(async () => {
    // If already fetching, don't start another fetch
    if (isFetchingRef.current) {
      return;
    }

    isFetchingRef.current = true;
    setIsLoading(true);

    try {
      const response = await fetch("/ui/api/v1/cluster/nodes");
      if (!response.ok) {
        throw new Error(`Failed to fetch cluster data: ${response.statusText}`);
      }
      const data = await response.json();
      setCluster(data);
      setError(null);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "An unknown error occurred"
      );
    } finally {
      setIsLoading(false);
      isFetchingRef.current = false;
    }
  }, []);

  const refresh = useCallback(async () => {
    await fetchCluster();
  }, [fetchCluster]);

  // Fetch only once when the provider mounts
  useEffect(() => {
    fetchCluster();
  }, [fetchCluster]);

  return (
    <ClusterContext.Provider value={{ cluster, error, isLoading, refresh }}>
      {children}
    </ClusterContext.Provider>
  );
}

export function useCluster(): ClusterContextValue {
  const context = useContext(ClusterContext);
  if (context === undefined) {
    throw new Error("useCluster must be used within a ClusterProvider");
  }
  return context;
}
