import React, { createContext, useContext, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";

interface FeatureFlags {
  goldfish: boolean;
}

interface FeatureFlagsContextType {
  features: FeatureFlags;
  isLoading: boolean;
  error: Error | null;
}

const defaultFeatures: FeatureFlags = {
  goldfish: false,
};

const FeatureFlagsContext = createContext<FeatureFlagsContextType>({
  features: defaultFeatures,
  isLoading: true,
  error: null,
});

export function useFeatureFlags() {
  return useContext(FeatureFlagsContext);
}

// For development mode, check if we have an override
function getDevOverrides(): Partial<FeatureFlags> {
  if (import.meta.env.DEV) {
    const overrides: Partial<FeatureFlags> = {};
    
    // Check for VITE_ENABLE_GOLDFISH environment variable
    if (import.meta.env.VITE_ENABLE_GOLDFISH === 'true') {
      overrides.goldfish = true;
    }
    
    return overrides;
  }
  return {};
}

async function fetchFeatures(): Promise<FeatureFlags> {
  // In development mode with no backend, use defaults with overrides
  if (import.meta.env.DEV && import.meta.env.VITE_MOCK_FEATURES === 'true') {
    return { ...defaultFeatures, ...getDevOverrides() };
  }
  
  const response = await fetch('/ui/api/v1/features');
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