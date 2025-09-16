import React, { createContext } from "react";
import { useQuery } from "@tanstack/react-query";
import { absolutePath } from "../util";
import { shouldUseMockFeatures, getDevEnvironmentOverrides } from "../lib/environment";

interface GoldfishFeature {
  enabled: boolean;
  cellANamespace?: string;
  cellBNamespace?: string;
}

interface FeatureFlags {
  goldfish: GoldfishFeature;
}

interface FeatureFlagsContextType {
  features: FeatureFlags;
  isLoading: boolean;
  error: Error | null;
}

const defaultFeatures: FeatureFlags = {
  goldfish: {
    enabled: false,
  },
};

export const FeatureFlagsContext = createContext<FeatureFlagsContextType>({
  features: defaultFeatures,
  isLoading: true,
  error: null,
});


// For development mode, check if we have an override
function getDevOverrides(): Partial<FeatureFlags> {
  return getDevEnvironmentOverrides() as Partial<FeatureFlags>;
}

async function fetchFeatures(): Promise<FeatureFlags> {
  // In development mode with no backend, use defaults with overrides
  if (shouldUseMockFeatures()) {
    return { ...defaultFeatures, ...getDevOverrides() };
  }
  
  const response = await fetch(absolutePath('/api/v1/features'));
  if (!response.ok) {
    throw new Error(`Failed to fetch features: ${response.statusText}`);
  }
  
  const data = await response.json();
  return { ...defaultFeatures, ...data, ...getDevOverrides() };
}

export function FeatureFlagsProvider({ children }: { children: React.ReactNode }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['feature-flags'],
    queryFn: fetchFeatures,
    staleTime: Infinity, // Feature flags don't change during runtime
    retry: 1,
  });
  
  const value: FeatureFlagsContextType = {
    features: data || defaultFeatures,
    isLoading,
    error: error as Error | null,
  };
  
  return (
    <FeatureFlagsContext.Provider value={value}>
      {children}
    </FeatureFlagsContext.Provider>
  );
}
