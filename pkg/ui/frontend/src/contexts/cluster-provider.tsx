import { useState, useCallback, useRef, useEffect, ReactNode } from "react";
import { Cluster } from "@/types/cluster";
import { ClusterContext } from "./cluster-context";
import { absolutePath } from "@/util";

interface ClusterProviderProps {
  children: ReactNode;
}

export function ClusterProvider({ children }: ClusterProviderProps) {
  const [cluster, setCluster] = useState<Cluster | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const isFetchingRef = useRef(false);

  const fetchCluster = useCallback(async () => {
    if (isFetchingRef.current) {
      return;
    }

    isFetchingRef.current = true;
    setIsLoading(true);

    try {
      const response = await fetch(absolutePath("/api/v1/cluster/nodes"));
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

  useEffect(() => {
    fetchCluster();
  }, [fetchCluster]);

  return (
    <ClusterContext.Provider value={{ cluster, error, isLoading, refresh }}>
      {children}
    </ClusterContext.Provider>
  );
}
