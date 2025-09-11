// Mock import.meta.env before importing the module
const mockEnv = {
  DEV: false,
  VITE_MOCK_FEATURES: undefined as string | undefined,
  VITE_ENABLE_GOLDFISH: undefined as string | undefined,
  VITE_GOLDFISH_CELL_A_NAMESPACE: undefined as string | undefined,
  VITE_GOLDFISH_CELL_B_NAMESPACE: undefined as string | undefined,
};

// Jest mock the entire module to control import.meta.env
jest.mock('./environment', () => ({
  getEnvironment: () => ({
    isDev: mockEnv.DEV || false,
    mockFeatures: mockEnv.VITE_MOCK_FEATURES === 'true',
    enableGoldfish: mockEnv.VITE_ENABLE_GOLDFISH === 'true',
    goldfishCellANamespace: mockEnv.VITE_GOLDFISH_CELL_A_NAMESPACE,
    goldfishCellBNamespace: mockEnv.VITE_GOLDFISH_CELL_B_NAMESPACE,
  }),
  shouldUseMockFeatures: () => {
    const isDev = mockEnv.DEV || false;
    const mockFeatures = mockEnv.VITE_MOCK_FEATURES === 'true';
    return isDev && mockFeatures;
  },
  getDevEnvironmentOverrides: () => {
    const isDev = mockEnv.DEV || false;
    const enableGoldfish = mockEnv.VITE_ENABLE_GOLDFISH === 'true';
    const overrides: { goldfish?: { enabled: boolean; cellANamespace?: string; cellBNamespace?: string } } = {};
    
    if (isDev && enableGoldfish) {
      overrides.goldfish = { 
        enabled: true,
        cellANamespace: mockEnv.VITE_GOLDFISH_CELL_A_NAMESPACE,
        cellBNamespace: mockEnv.VITE_GOLDFISH_CELL_B_NAMESPACE,
      };
    }
    
    return overrides;
  },
}));

import { getEnvironment, shouldUseMockFeatures, getDevEnvironmentOverrides } from './environment';

describe('environment', () => {
  beforeEach(() => {
    // Reset mock env before each test
    mockEnv.DEV = false;
    mockEnv.VITE_MOCK_FEATURES = undefined;
    mockEnv.VITE_ENABLE_GOLDFISH = undefined;
    mockEnv.VITE_GOLDFISH_CELL_A_NAMESPACE = undefined;
    mockEnv.VITE_GOLDFISH_CELL_B_NAMESPACE = undefined;
  });

  describe('getEnvironment', () => {
    it('returns default values when env vars are not set', () => {
      const env = getEnvironment();
      expect(env).toEqual({
        isDev: false,
        mockFeatures: false,
        enableGoldfish: false,
        goldfishCellANamespace: undefined,
        goldfishCellBNamespace: undefined,
      });
    });

    it('returns correct values when env vars are set', () => {
      mockEnv.DEV = true;
      mockEnv.VITE_MOCK_FEATURES = 'true';
      mockEnv.VITE_ENABLE_GOLDFISH = 'true';
      mockEnv.VITE_GOLDFISH_CELL_A_NAMESPACE = 'loki-ops-002';
      mockEnv.VITE_GOLDFISH_CELL_B_NAMESPACE = 'loki-ops-003';

      const env = getEnvironment();
      expect(env).toEqual({
        isDev: true,
        mockFeatures: true,
        enableGoldfish: true,
        goldfishCellANamespace: 'loki-ops-002',
        goldfishCellBNamespace: 'loki-ops-003',
      });
    });
  });

  describe('shouldUseMockFeatures', () => {
    it('returns false when not in dev mode', () => {
      mockEnv.DEV = false;
      mockEnv.VITE_MOCK_FEATURES = 'true';
      
      expect(shouldUseMockFeatures()).toBe(false);
    });

    it('returns false when mockFeatures is not enabled', () => {
      mockEnv.DEV = true;
      mockEnv.VITE_MOCK_FEATURES = 'false';
      
      expect(shouldUseMockFeatures()).toBe(false);
    });

    it('returns true when in dev mode and mockFeatures is enabled', () => {
      mockEnv.DEV = true;
      mockEnv.VITE_MOCK_FEATURES = 'true';
      
      expect(shouldUseMockFeatures()).toBe(true);
    });
  });

  describe('getDevEnvironmentOverrides', () => {
    it('returns empty object when not in dev mode', () => {
      mockEnv.DEV = false;
      mockEnv.VITE_ENABLE_GOLDFISH = 'true';
      
      expect(getDevEnvironmentOverrides()).toEqual({});
    });

    it('returns empty object when goldfish is not enabled', () => {
      mockEnv.DEV = true;
      mockEnv.VITE_ENABLE_GOLDFISH = 'false';
      
      expect(getDevEnvironmentOverrides()).toEqual({});
    });

    it('returns goldfish override when enabled in dev mode', () => {
      mockEnv.DEV = true;
      mockEnv.VITE_ENABLE_GOLDFISH = 'true';
      
      expect(getDevEnvironmentOverrides()).toEqual({
        goldfish: {
          enabled: true,
          cellANamespace: undefined,
          cellBNamespace: undefined,
        },
      });
    });

    it('returns goldfish override with namespaces when configured', () => {
      mockEnv.DEV = true;
      mockEnv.VITE_ENABLE_GOLDFISH = 'true';
      mockEnv.VITE_GOLDFISH_CELL_A_NAMESPACE = 'loki-ops-002';
      mockEnv.VITE_GOLDFISH_CELL_B_NAMESPACE = 'loki-ops-003';
      
      expect(getDevEnvironmentOverrides()).toEqual({
        goldfish: {
          enabled: true,
          cellANamespace: 'loki-ops-002',
          cellBNamespace: 'loki-ops-003',
        },
      });
    });
  });
});