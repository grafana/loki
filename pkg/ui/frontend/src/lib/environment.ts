/**
 * Environment abstraction layer to make import.meta.env testable
 * This provides a clean interface for accessing environment variables
 * that can be easily mocked in tests.
 */

export interface Environment {
  isDev: boolean;
  mockFeatures: boolean;
  enableGoldfish?: boolean;
}

/**
 * Get the current environment configuration
 * In tests, this can be mocked to return test-specific values
 */
export function getEnvironment(): Environment {
  // In production/development, use import.meta.env
  // In tests, this function will be mocked
  return {
    isDev: import.meta.env.DEV || false,
    mockFeatures: import.meta.env.VITE_MOCK_FEATURES === 'true',
    enableGoldfish: import.meta.env.VITE_ENABLE_GOLDFISH === 'true',
  };
}

/**
 * Check if we should use mock features
 * This is the main function used by the feature flags system
 */
export function shouldUseMockFeatures(): boolean {
  const env = getEnvironment();
  return env.isDev && env.mockFeatures;
}

/**
 * Get development overrides for feature flags
 * This replaces the direct import.meta.env access in getDevOverrides
 */
export function getDevEnvironmentOverrides(): { goldfish?: boolean } {
  const env = getEnvironment();
  const overrides: { goldfish?: boolean } = {};
  
  if (env.isDev && env.enableGoldfish) {
    overrides.goldfish = true;
  }
  
  return overrides;
}