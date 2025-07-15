import { useCallback, useRef } from "react";
import { absolutePath } from "@/util";
interface MetricSample {
  timestamp: number;
  values: Record<string, number>;
}

interface RateMetric {
  name: string;
  rate: number;
  currentValue: number;
}

interface UseRateNodeMetricsOptions {
  nodeNames: string[];
  metrics: string[];
}

interface NodeMetrics {
  [nodeId: string]: RateMetric[];
}
export const useRateNodeMetrics = () => {
  const previousSamplesRef = useRef<Record<string, MetricSample>>({});

  const fetchMetrics = useCallback(
    async ({
      nodeNames,
      metrics,
    }: UseRateNodeMetricsOptions): Promise<NodeMetrics> => {
      if (!nodeNames.length) {
        return {};
      }

      const currentSamples: Record<string, MetricSample> = {
        ...previousSamplesRef.current,
      };
      const newRates: NodeMetrics = {};

      // Fetch metrics for all nodes in parallel
      await Promise.all(
        nodeNames.map(async (nodeName) => {
          try {
            const response = await fetch(
              absolutePath(`/api/v1/proxy/${nodeName}/metrics`)
            );

            if (!response.ok) {
              throw new Error(
                `Failed to fetch metrics: ${response.statusText}`
              );
            }

            const text = await response.text();
            const currentSample: MetricSample = {
              timestamp: Date.now(),
              values: {},
            };

            // Parse the metrics text and extract values
            metrics.forEach((metricName) => {
              const regex = new RegExp(
                `${metricName}\\{[^}]*\\}\\s+([\\d.e+]+)`
              );
              const match = text.match(regex);
              if (match) {
                currentSample.values[metricName] = parseFloat(match[1]);
              }
            });

            const previousSample = previousSamplesRef.current[nodeName];
            currentSamples[nodeName] = currentSample;

            // Calculate rates if we have a previous sample
            if (previousSample) {
              const timeDiffSeconds =
                (currentSample.timestamp - previousSample.timestamp) / 1000;

              // Only calculate rates if we have a meaningful time difference
              if (timeDiffSeconds > 0) {
                const nodeRates = metrics.map((metricName) => {
                  const currentValue = currentSample.values[metricName];
                  const previousValue = previousSample.values[metricName];

                  if (
                    currentValue !== undefined &&
                    previousValue !== undefined
                  ) {
                    const rate =
                      (currentValue - previousValue) / timeDiffSeconds;
                    return {
                      name: metricName,
                      rate,
                      currentValue,
                    };
                  }

                  return {
                    name: metricName,
                    rate: 0,
                    currentValue: currentValue ?? 0,
                  };
                });

                newRates[nodeName] = nodeRates;
              }
            }
          } catch (err) {
            console.error(`Error fetching metrics for node ${nodeName}:`, err);
          }
        })
      );

      previousSamplesRef.current = currentSamples;
      return newRates;
    },
    []
  );

  return {
    fetchMetrics,
  };
};
