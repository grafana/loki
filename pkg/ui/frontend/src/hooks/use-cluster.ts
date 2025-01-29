import { useState, useCallback } from "react";
import { Cluster } from "../types/cluster";

interface UseClusterReturn {
  cluster: Cluster | null;
  error: string | null;
  fetchCluster: () => Promise<void>;
  isLoading: boolean;
}

export function useCluster(): UseClusterReturn {
  const [cluster, setCluster] = useState<Cluster | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const fetchCluster = useCallback(async () => {
    setIsLoading(true);
    try {
      const response = await fetch("/ui/api/v1/cluster");
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
    }
  }, []);

  return { cluster, error, fetchCluster, isLoading };
}
