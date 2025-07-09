import { absolutePath } from "@/util";
import { useState, useEffect } from "react";

interface UseNodeMetricsResult {
  isLoading: boolean;
  error: string | null;
  metrics: string;
}

export function useNodeMetrics(
  nodeName: string | undefined,
  isMetricsTabActive: boolean
): UseNodeMetricsResult {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [metrics, setMetrics] = useState<string>("");

  useEffect(() => {
    // Reset state when tab becomes inactive
    if (!isMetricsTabActive) {
      setMetrics("");
      return;
    }

    // Don't fetch if nodeName is undefined
    if (!nodeName) {
      return;
    }

    const abortController = new AbortController();

    async function fetchMetrics() {
      setIsLoading(true);
      setError(null);

      try {
        const response = await fetch(
          absolutePath(`/api/v1/proxy/${nodeName}/metrics`),
          {
            signal: abortController.signal,
          }
        );

        if (!response.ok) {
          throw new Error(`Failed to fetch metrics: ${response.statusText}`);
        }

        const metricsData = await response.text();
        setMetrics(metricsData);
      } catch (err) {
        if (err instanceof Error) {
          setError(err.message);
        } else {
          setError("An unknown error occurred");
        }
      } finally {
        setIsLoading(false);
      }
    }

    fetchMetrics();

    // Cleanup function to abort fetch if component unmounts or dependencies change
    return () => {
      abortController.abort();
    };
  }, [nodeName, isMetricsTabActive]); // Only re-run if nodeName or tab active state changes

  return {
    isLoading,
    error,
    metrics,
  };
}
