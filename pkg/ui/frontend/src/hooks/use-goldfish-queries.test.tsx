import { renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useGoldfishQueries } from './use-goldfish-queries';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OUTCOME_ALL, OUTCOME_MATCH } from '@/types/goldfish';
import React from 'react';

/**
 * @jest-environment jsdom
 */

// Mock the API module
jest.mock('@/lib/goldfish-api', () => ({
  fetchSampledQueries: jest.fn(),
}));

const mockFetchSampledQueries = fetchSampledQueries as jest.MockedFunction<typeof fetchSampledQueries>;

// Create a wrapper with QueryClient
const createWrapper = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
};

describe('useGoldfishQueries', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('filter handling', () => {
    it('passes tenant filter to API call', async () => {
      const mockData = {
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce(mockData);

      const { result } = renderHook(() => 
        useGoldfishQueries(1, 20, OUTCOME_ALL, 'tenant-xyz'),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 20, OUTCOME_ALL, 'tenant-xyz', undefined, undefined);
    });

    it('passes user filter to API call', async () => {
      const mockData = {
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce(mockData);

      const { result } = renderHook(() => 
        useGoldfishQueries(1, 20, OUTCOME_ALL, undefined, 'alice'),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 20, OUTCOME_ALL, undefined, 'alice', undefined);
    });

    it('passes newEngine filter to API call', async () => {
      const mockData = {
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce(mockData);

      const { result } = renderHook(() => 
        useGoldfishQueries(1, 20, OUTCOME_ALL, undefined, undefined, true),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 20, OUTCOME_ALL, undefined, undefined, true);
    });

    it('passes all filters to API call', async () => {
      const mockData = {
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce(mockData);

      const { result } = renderHook(() => 
        useGoldfishQueries(2, 50, OUTCOME_MATCH, 'tenant-b', 'bob', false),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 50, OUTCOME_MATCH, 'tenant-b', 'bob', false);
    });

    it('refetches data when filters change', async () => {
      const mockData = {
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValue(mockData);

      const { result, rerender } = renderHook(
        ({ tenant, user, newEngine }: { tenant?: string; user?: string; newEngine?: boolean }) => 
          useGoldfishQueries(1, 20, OUTCOME_ALL, tenant, user, newEngine),
        {
          initialProps: { tenant: undefined, user: undefined, newEngine: undefined },
          wrapper: createWrapper(),
        }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(1, 20, OUTCOME_ALL, undefined, undefined, undefined);

      // Change tenant filter
      rerender({ tenant: 'tenant-new', user: undefined, newEngine: undefined });

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);
      });

      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(1, 20, OUTCOME_ALL, 'tenant-new', undefined, undefined);

      // Change user filter
      rerender({ tenant: 'tenant-new', user: 'charlie', newEngine: undefined });

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(3);
      });

      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(1, 20, OUTCOME_ALL, 'tenant-new', 'charlie', undefined);

      // Change newEngine filter
      rerender({ tenant: 'tenant-new', user: 'charlie', newEngine: true });

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(4);
      });

      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(1, 20, OUTCOME_ALL, 'tenant-new', 'charlie', true);
    });
  });

  describe('existing functionality', () => {
    it('returns loading state initially', () => {
      mockFetchSampledQueries.mockResolvedValueOnce({
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      });

      const { result } = renderHook(() => useGoldfishQueries(1, 20, OUTCOME_ALL), { wrapper: createWrapper() });

      expect(result.current.isLoading).toBe(true);
      expect(result.current.error).toBeNull();
      expect(result.current.data).toBeUndefined();
    });

    it('returns data on successful fetch', async () => {
      const mockData = {
        queries: [
          {
            correlationId: '123',
            tenantId: 'tenant1',
            user: 'alice',
            query: 'test query',
            queryType: 'query_range',
            startTime: '2023-01-01T00:00:00Z',
            endTime: '2023-01-01T01:00:00Z',
            stepDuration: 15000,
            cellAExecTimeMs: 100,
            cellBExecTimeMs: 150,
            cellAQueueTimeMs: 5,
            cellBQueueTimeMs: 6,
            cellABytesProcessed: 1000,
            cellBBytesProcessed: 1100,
            cellALinesProcessed: 50,
            cellBLinesProcessed: 55,
            cellABytesPerSecond: 1000,
            cellBBytesPerSecond: 1100,
            cellALinesPerSecond: 50,
            cellBLinesPerSecond: 55,
            cellAEntriesReturned: 10,
            cellBEntriesReturned: 12,
            cellASplits: 1,
            cellBSplits: 1,
            cellAShards: 2,
            cellBShards: 2,
            cellAResponseHash: 'hash1',
            cellBResponseHash: 'hash1',
            cellAResponseSize: 1000,
            cellBResponseSize: 1000,
            cellAStatusCode: 200,
            cellBStatusCode: 200,
            cellATraceID: 'trace1',
            cellBTraceID: 'trace2',
            sampledAt: '2023-01-01T02:00:00Z',
            createdAt: '2023-01-01T02:00:00Z',
            comparisonStatus: 'match',
            cellAUsedNewEngine: false,
            cellBUsedNewEngine: false,
          },
        ],
        total: 1,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce(mockData);

      const { result } = renderHook(() => useGoldfishQueries(1, 20, OUTCOME_ALL), { wrapper: createWrapper() });

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(result.current.data).toEqual(mockData);
      expect(result.current.error).toBeNull();
    });

    it('returns error on failed fetch', async () => {
      const error = new Error('Network error');
      mockFetchSampledQueries.mockRejectedValueOnce(error);

      const { result } = renderHook(() => useGoldfishQueries(1, 20, OUTCOME_ALL), { wrapper: createWrapper() });

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(result.current.error).toBe(error);
      expect(result.current.data).toBeUndefined();
    });

    it('refetches data when page changes', async () => {
      const mockData = {
        queries: [],
        total: 0,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValue(mockData);

      const { result, rerender } = renderHook(
        ({ page }) => useGoldfishQueries(page, 20, OUTCOME_ALL),
        {
          initialProps: { page: 1 },
          wrapper: createWrapper(),
        }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);

      rerender({ page: 2 });

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);
      });

      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(2, 20, OUTCOME_ALL, undefined, undefined, undefined);
    });
  });
});