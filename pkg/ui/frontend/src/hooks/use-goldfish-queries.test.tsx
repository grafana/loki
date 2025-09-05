import { renderHook, waitFor, act } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useGoldfishQueries } from './use-goldfish-queries';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, type GoldfishAPIResponse } from '@/types/goldfish';
import type { FetchResult } from '@/lib/goldfish-api';
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
        staleTime: 0,
      },
    },
  });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
};

// Mock data
const mockQueries = [
  {
    correlationId: 'test-1',
    tenantId: 'tenant-a',
    user: 'user1@example.com',
    comparisonStatus: 'match' as const,
    query: 'test query 1',
    queryType: 'instant' as const,
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 110,
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
    cellAResponseHash: 'hash-a',
    cellBResponseHash: 'hash-b',
    cellAResponseSize: 500,
    cellBResponseSize: 550,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: 'trace-a',
    cellBTraceID: 'trace-b',
    cellASpanID: 'span-a',
    cellBSpanID: 'span-b',
    cellAUsedNewEngine: false,
    cellBUsedNewEngine: false,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
  {
    correlationId: 'test-2',
    tenantId: 'tenant-b',
    user: 'user2@example.com',
    comparisonStatus: 'mismatch' as const,
    query: 'test query 2',
    queryType: 'instant' as const,
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 200,
    cellBExecTimeMs: 250,
    cellAQueueTimeMs: 10,
    cellBQueueTimeMs: 12,
    cellABytesProcessed: 2000,
    cellBBytesProcessed: 2200,
    cellALinesProcessed: 100,
    cellBLinesProcessed: 110,
    cellABytesPerSecond: 2000,
    cellBBytesPerSecond: 2200,
    cellALinesPerSecond: 100,
    cellBLinesPerSecond: 110,
    cellAEntriesReturned: 20,
    cellBEntriesReturned: 22,
    cellASplits: 2,
    cellBSplits: 2,
    cellAShards: 4,
    cellBShards: 4,
    cellAResponseHash: 'hash-a2',
    cellBResponseHash: 'hash-b2',
    cellAResponseSize: 1000,
    cellBResponseSize: 1100,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: 'trace-a2',
    cellBTraceID: 'trace-b2',
    cellASpanID: 'span-a2',
    cellBSpanID: 'span-b2',
    cellAUsedNewEngine: false,
    cellBUsedNewEngine: true,
    sampledAt: '2024-01-01T01:00:00Z',
    createdAt: '2024-01-01T01:00:00Z',
  },
];

describe('useGoldfishQueries', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  describe('initial loading', () => {
    it('loads initial page correctly', async () => {
      const mockData = {
        queries: mockQueries,
        hasMore: true,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce({ data: mockData, traceId: 'test-trace' });

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_ALL),
        { wrapper: createWrapper() }
      );

      // Initially loading
      expect(result.current.isLoading).toBe(true);
      expect(result.current.queries).toEqual([]);

      // Wait for data to load
      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      // Should have the queries sorted by sampledAt descending
      expect(result.current.queries).toEqual([mockQueries[1], mockQueries[0]]);
      expect(result.current.hasMore).toBe(true);
      expect(result.current.traceId).toBe('test-trace');
    });

    it('handles initial load error', async () => {
      const error = new Error('Network error');
      mockFetchSampledQueries.mockRejectedValueOnce(error);

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_ALL),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(result.current.error).toBe(error);
      expect(result.current.queries).toEqual([]);
    });
  });

  describe('load more functionality', () => {
    it('accumulates data when loadMore is called', async () => {
      const page1Data = {
        queries: [mockQueries[0]],
        hasMore: true,
        page: 1,
        pageSize: 20,
      };
      const page2Data = {
        queries: [mockQueries[1]],
        hasMore: false,
        page: 2,
        pageSize: 20,
      };
      
      mockFetchSampledQueries
        .mockResolvedValueOnce({ data: page1Data, traceId: 'trace-1' })
        .mockResolvedValueOnce({ data: page2Data, traceId: 'trace-2' });

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_ALL),
        { wrapper: createWrapper() }
      );

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(result.current.queries).toEqual([mockQueries[0]]);
      expect(result.current.hasMore).toBe(true);

      // Call loadMore
      act(() => {
        result.current.loadMore();
      });

      // Should be loading more
      expect(result.current.isLoadingMore).toBe(true);

      // Wait for second page to load
      await waitFor(() => {
        expect(result.current.isLoadingMore).toBe(false);
      });

      // Should have both queries accumulated and sorted
      expect(result.current.queries).toEqual([mockQueries[1], mockQueries[0]]);
      expect(result.current.hasMore).toBe(false);
      expect(result.current.traceId).toBe('trace-2');
    });

    it('does not load more when already loading', async () => {
      const page1Data = {
        queries: [mockQueries[0]],
        hasMore: true,
        page: 1,
        pageSize: 20,
      };
      
      // Delay the second response
      let resolveSecondCall: ((value: unknown) => void) | undefined;
      const secondCallPromise = new Promise(resolve => {
        resolveSecondCall = resolve;
      });
      
      mockFetchSampledQueries
        .mockResolvedValueOnce({ data: page1Data, traceId: 'trace-1' })
        .mockImplementationOnce(() => secondCallPromise as unknown as Promise<FetchResult<GoldfishAPIResponse>>);

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_ALL),
        { wrapper: createWrapper() }
      );

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);

      // Call loadMore twice
      act(() => {
        result.current.loadMore();
        result.current.loadMore(); // This should be ignored
      });

      // Should only call API once more (2 total), not 3 because we're ignoring the second call
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);

      // Resolve the second call
      resolveSecondCall!({ data: { queries: [], hasMore: false, page: 2, pageSize: 20 }, traceId: 'trace-2' });

      await waitFor(() => {
        expect(result.current.isLoadingMore).toBe(false);
      });
    });
  });

  describe('filter changes', () => {
    it('resets to page 1 when filters change', async () => {
      const initialData = {
        queries: mockQueries,
        hasMore: true,
        page: 1,
        pageSize: 20,
      };
      const page2Data = {
        queries: [mockQueries[1]],
        hasMore: false,
        page: 2,
        pageSize: 20,
      };
      const filteredData = {
        queries: [mockQueries[0]],
        hasMore: false,
        page: 1,
        pageSize: 20,
      };

      mockFetchSampledQueries
        .mockResolvedValueOnce({ data: initialData, traceId: 'trace-1' })
        .mockResolvedValueOnce({ data: page2Data, traceId: 'trace-2' })
        .mockResolvedValueOnce({ data: filteredData, traceId: 'trace-3' })
        .mockResolvedValue({ data: filteredData, traceId: 'trace-4' }); // Default for any additional calls

      const { result, rerender } = renderHook(
        ({ tenant }) => useGoldfishQueries(20, OUTCOME_ALL, tenant),
        { 
          wrapper: createWrapper(),
          initialProps: { tenant: undefined as string | undefined }
        }
      );

      // Wait for initial load
      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      // Load more to get page 2
      act(() => {
        result.current.loadMore();
      });

      await waitFor(() => {
        expect(result.current.isLoadingMore).toBe(false);
      });

      // Should have accumulated queries (2 from page 1 + 1 from page 2 = 3, sorted)
      expect(result.current.queries).toHaveLength(3);

      // Change filter
      act(() => {
        rerender({ tenant: 'tenant-a' });
      });

      // Wait for the query to be called with the new filter
      await waitFor(() => {
        expect(mockFetchSampledQueries.mock.calls.length).toBeGreaterThanOrEqual(3);
        const lastCall = mockFetchSampledQueries.mock.calls[mockFetchSampledQueries.mock.calls.length - 1];
        expect(lastCall[2]).toBe('tenant-a'); // Check tenant filter was applied
      });

      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(
        1, // Page 1
        20,
        'tenant-a',
        undefined,
        undefined,
        undefined,
        undefined
      );

      // Wait for the new data to be loaded and queries to be updated
      await waitFor(() => {
        // Check that we're not loading anymore and have data
        expect(result.current.isLoading).toBe(false);
      });
      
      // Give it a moment for the data to be processed
      await waitFor(() => {
        // The filtered data should have one query
        expect(result.current.queries.length).toBeGreaterThan(0);
        expect(result.current.queries).toHaveLength(1);
      }, { timeout: 3000 });
      
      // Now check the results
      expect(result.current.queries[0].correlationId).toBe('test-1');
    });

    it('handles time range changes', async () => {
      const from = new Date('2024-01-01T00:00:00Z');
      const to = new Date('2024-01-02T00:00:00Z');

      mockFetchSampledQueries.mockResolvedValue({ 
        data: { queries: mockQueries, hasMore: false, page: 1, pageSize: 20 },
        traceId: 'test-trace'
      });

      const { result, rerender } = renderHook(
        ({ from, to }) => useGoldfishQueries(20, OUTCOME_ALL, undefined, undefined, undefined, from, to),
        { 
          wrapper: createWrapper(),
          initialProps: { from: null as Date | null, to: null as Date | null }
        }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      // Change time range
      rerender({ from, to });

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);
      });

      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(
        1,
        20,
        undefined,
        undefined,
        undefined,
        from,
        to
      );
    });
  });

  describe('frontend filtering', () => {
    it('filters by outcome on frontend', async () => {
      const mockData = {
        queries: mockQueries,
        hasMore: false,
        page: 1,
        pageSize: 20,
      };
      mockFetchSampledQueries.mockResolvedValueOnce({ data: mockData, traceId: 'test-trace' });

      // Test with MATCH filter
      const { result, rerender } = renderHook(
        ({ outcome }) => useGoldfishQueries(20, outcome),
        { 
          wrapper: createWrapper(),
          initialProps: { outcome: OUTCOME_MATCH as typeof OUTCOME_MATCH | typeof OUTCOME_MISMATCH | typeof OUTCOME_ALL }
        }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      // Should only show match queries
      expect(result.current.queries).toEqual([mockQueries[0]]);

      // Change to MISMATCH
      rerender({ outcome: OUTCOME_MISMATCH });

      // Should immediately filter to mismatch queries (no new API call for outcome filter)
      expect(result.current.queries).toEqual([mockQueries[1]]);
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1); // No additional API call

      // Change to ALL
      rerender({ outcome: OUTCOME_ALL });

      // Should show all queries sorted
      expect(result.current.queries).toEqual([mockQueries[1], mockQueries[0]]);
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1); // Still no additional API call
    });

    it('maintains outcome filter when loading more', async () => {
      const page1Data = {
        queries: mockQueries,
        hasMore: true,
        page: 1,
        pageSize: 20,
      };
      const page2Data = {
        queries: [
          { ...mockQueries[0], correlationId: 'test-3' },
          { ...mockQueries[1], correlationId: 'test-4' },
        ],
        hasMore: false,
        page: 2,
        pageSize: 20,
      };

      mockFetchSampledQueries
        .mockResolvedValueOnce({ data: page1Data, traceId: 'trace-1' })
        .mockResolvedValueOnce({ data: page2Data, traceId: 'trace-2' });

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_MATCH),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      // Should only show match from page 1
      expect(result.current.queries).toHaveLength(1);
      expect(result.current.queries[0].comparisonStatus).toBe('match');

      // Load more
      act(() => {
        result.current.loadMore();
      });

      await waitFor(() => {
        expect(result.current.isLoadingMore).toBe(false);
      });

      // Should only show match queries from both pages
      expect(result.current.queries).toHaveLength(2);
      expect(result.current.queries.every(q => q.comparisonStatus === 'match')).toBe(true);
    });
  });

  describe('refresh functionality', () => {
    it('refreshes data when refresh is called', async () => {
      const initialData = {
        queries: [mockQueries[0]],
        hasMore: true,
        page: 1,
        pageSize: 20,
      };
      const refreshedData = {
        queries: mockQueries,
        hasMore: false,
        page: 1,
        pageSize: 20,
      };

      mockFetchSampledQueries
        .mockResolvedValueOnce({ data: initialData, traceId: 'trace-1' })
        .mockResolvedValueOnce({ data: refreshedData, traceId: 'trace-2' });

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_ALL),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      expect(result.current.queries).toEqual([mockQueries[0]]);

      // Call refresh
      act(() => {
        result.current.refresh();
      });

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);
      });

      // Should reset to page 1
      expect(mockFetchSampledQueries).toHaveBeenLastCalledWith(
        1,
        20,
        undefined,
        undefined,
        undefined,
        undefined,
        undefined
      );

      await waitFor(() => {
        expect(result.current.queries).toEqual([mockQueries[1], mockQueries[0]]);
      });
    });
  });

  describe('sorting', () => {
    it('sorts queries by sampledAt in descending order', async () => {
      const unsortedQueries = [
        { ...mockQueries[0], sampledAt: '2024-01-01T00:00:00Z' },
        { ...mockQueries[1], sampledAt: '2024-01-01T02:00:00Z' },
        { ...mockQueries[0], correlationId: 'test-3', sampledAt: '2024-01-01T01:00:00Z' },
      ];

      mockFetchSampledQueries.mockResolvedValueOnce({ 
        data: { queries: unsortedQueries, hasMore: false, page: 1, pageSize: 20 },
        traceId: 'test-trace'
      });

      const { result } = renderHook(
        () => useGoldfishQueries(20, OUTCOME_ALL),
        { wrapper: createWrapper() }
      );

      await waitFor(() => {
        expect(result.current.isLoading).toBe(false);
      });

      // Should be sorted by sampledAt descending
      expect(result.current.queries[0].sampledAt).toBe('2024-01-01T02:00:00Z');
      expect(result.current.queries[1].sampledAt).toBe('2024-01-01T01:00:00Z');
      expect(result.current.queries[2].sampledAt).toBe('2024-01-01T00:00:00Z');
    });
  });
});
