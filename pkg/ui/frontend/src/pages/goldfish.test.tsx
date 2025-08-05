import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import GoldfishPage from './goldfish';
import * as goldfishApi from '@/lib/goldfish-api';
import { OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR } from '@/types/goldfish';
import '@testing-library/jest-dom';

// Mock the goldfish API
jest.mock('@/lib/goldfish-api');
const mockFetchSampledQueries = goldfishApi.fetchSampledQueries as jest.MockedFunction<typeof goldfishApi.fetchSampledQueries>;

// Mock React Router
jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useNavigate: () => jest.fn(),
}));

// Mock PageContainer
jest.mock('@/layout/page-container', () => ({
  PageContainer: ({ children }: { children: React.ReactNode }) => <div data-testid="page-container">{children}</div>,
}));

// Mock QueryDiffView
jest.mock('@/components/goldfish/query-diff-view', () => ({
  QueryDiffView: ({ query }: { query: any }) => (
    <div data-testid="query-diff-view" data-correlation-id={query.correlationId}>
      Status: {query.comparisonStatus}
      <div>{query.query}</div>
    </div>
  ),
}));

const mockQueries = [
  {
    correlationId: 'match-1',
    tenantId: 'tenant-a',
    comparisonStatus: 'match',
    query: 'test query 1',
    queryType: 'instant',
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
    cellAResponseHash: 'hash-a-1',
    cellBResponseHash: 'hash-b-1',
    cellAResponseSize: 500,
    cellBResponseSize: 550,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: 'trace-a-1',
    cellBTraceID: 'trace-b-1',
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
  {
    correlationId: 'mismatch-1',
    tenantId: 'tenant-b',
    comparisonStatus: 'mismatch',
    query: 'test query 2',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 200,
    cellAQueueTimeMs: 5,
    cellBQueueTimeMs: 10,
    cellABytesProcessed: 1000,
    cellBBytesProcessed: 2000,
    cellALinesProcessed: 50,
    cellBLinesProcessed: 100,
    cellABytesPerSecond: 1000,
    cellBBytesPerSecond: 2000,
    cellALinesPerSecond: 50,
    cellBLinesPerSecond: 100,
    cellAEntriesReturned: 10,
    cellBEntriesReturned: 20,
    cellASplits: 1,
    cellBSplits: 2,
    cellAShards: 2,
    cellBShards: 4,
    cellAResponseHash: 'hash-a-2',
    cellBResponseHash: 'hash-b-2',
    cellAResponseSize: 500,
    cellBResponseSize: 1000,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: 'trace-a-2',
    cellBTraceID: 'trace-b-2',
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
  {
    correlationId: 'error-1',
    tenantId: 'tenant-a',
    comparisonStatus: 'error',
    query: 'test query 3',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: null,
    cellBExecTimeMs: null,
    cellAQueueTimeMs: null,
    cellBQueueTimeMs: null,
    cellABytesProcessed: null,
    cellBBytesProcessed: null,
    cellALinesProcessed: null,
    cellBLinesProcessed: null,
    cellABytesPerSecond: null,
    cellBBytesPerSecond: null,
    cellALinesPerSecond: null,
    cellBLinesPerSecond: null,
    cellAEntriesReturned: null,
    cellBEntriesReturned: null,
    cellASplits: null,
    cellBSplits: null,
    cellAShards: null,
    cellBShards: null,
    cellAResponseHash: null,
    cellBResponseHash: null,
    cellAResponseSize: null,
    cellBResponseSize: null,
    cellAStatusCode: 500,
    cellBStatusCode: 500,
    cellATraceID: null,
    cellBTraceID: null,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
];

const renderGoldfishPage = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        staleTime: 0, // No stale time in tests for immediate loading
        gcTime: 10 * 60 * 1000, // 10 minutes for prefetch functionality
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <GoldfishPage />
    </QueryClientProvider>
  );
};

describe('GoldfishPage', () => {
  beforeEach(() => {
    mockFetchSampledQueries.mockResolvedValue({
      queries: mockQueries,
      total: 25, // More than pageSize to enable prefetching
      page: 1,
      pageSize: 10,
    });
  });

  afterEach(() => {
    jest.clearAllMocks();
  });

  describe('Step 3: Immediate Filter Response', () => {
    it('shows filtered results immediately when filter is clicked', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });

      // Initially should show all queries
      expect(screen.getByText('Status: match')).toBeInTheDocument();
      expect(screen.getByText('Status: mismatch')).toBeInTheDocument();
      expect(screen.getByText('Status: error')).toBeInTheDocument();

      // Click on Match filter
      fireEvent.click(screen.getByText('Match'));

      // Should show filtered results immediately, not loading state
      expect(screen.queryByText('Loading')).not.toBeInTheDocument();
      expect(screen.getByText('Status: match')).toBeInTheDocument();
      expect(screen.queryByText('Status: mismatch')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: error')).not.toBeInTheDocument();
    });

    it('shows different filtered results when switching between filters', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });

      // Click on Mismatch filter
      fireEvent.click(screen.getByText('Mismatch'));

      // Should show only mismatch results immediately
      expect(screen.queryByText('Loading')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: match')).not.toBeInTheDocument();
      expect(screen.getByText('Status: mismatch')).toBeInTheDocument();
      expect(screen.queryByText('Status: error')).not.toBeInTheDocument();

      // Click on Error filter
      fireEvent.click(screen.getByText('Error'));

      // Should show only error results immediately
      expect(screen.queryByText('Loading')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: match')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: mismatch')).not.toBeInTheDocument();
      expect(screen.getByText('Status: error')).toBeInTheDocument();
    });
  });

  describe('Step 4: No Refetch on Filter Change', () => {
    it('primary query does not refetch when filter selection changes', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Click on different filters
      fireEvent.click(screen.getByText('Match'));
      fireEvent.click(screen.getByText('Mismatch'));
      fireEvent.click(screen.getByText('Error'));
      fireEvent.click(screen.getByText('All'));

      // Primary query should still only have 1 call with OUTCOME_ALL
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL);
      
      // But background queries should also be called
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MISMATCH);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ERROR);
    });

    it('fetches data with OUTCOME_ALL regardless of selected filter', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Verify it was called with OUTCOME_ALL
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL);
    });
  });

  describe('Step 5: Background Server Request for Non-All Filters', () => {
    it('triggers background server request when specific outcome is selected', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Click on Match filter
      fireEvent.click(screen.getByText('Match'));

      // Should trigger background request with specific outcome
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH);
      });

      // Should have multiple calls (initial + background + prefetch)
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL);
    });

    it('triggers background server request when switching to different outcomes', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Click on Mismatch filter
      fireEvent.click(screen.getByText('Mismatch'));

      // Should trigger background request with mismatch outcome
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MISMATCH);
      });

      // Click on Error filter
      fireEvent.click(screen.getByText('Error'));

      // Should trigger background request with error outcome
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ERROR);
      });

      // Should have called with different outcomes
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MISMATCH);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ERROR);
    });

    it('does not trigger background request when selecting All filter', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Click on Match filter first
      fireEvent.click(screen.getByText('Match'));

      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH);
      });

      // Click on All filter
      fireEvent.click(screen.getByText('All'));

      // Should not trigger additional background request for "All"
      // because we already have all data
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL);
      
      // Should not make additional calls for "All" outcome background query
      // (but prefetch calls may still happen)
      const allCalls = mockFetchSampledQueries.mock.calls;
      const backgroundAllCalls = allCalls.filter(call => 
        call[0] === 1 && call[1] === 10 && call[2] === OUTCOME_ALL
      );
      expect(backgroundAllCalls).toHaveLength(1); // Only the initial call
    });
  });

  describe('Step 6: Server Response Updates UI', () => {
    it('updates UI when background server request completes', async () => {
      // Mock server to return different data for filtered request
      const mockFilteredQueries = [
        {
          correlationId: 'server-filtered-match',
          tenantId: 'tenant-server',
          comparisonStatus: 'match',
          query: 'server filtered query',
          queryType: 'instant',
          startTime: '2024-01-01T00:00:00Z',
          endTime: '2024-01-01T01:00:00Z',
          stepDuration: null,
          cellAExecTimeMs: 200,
          cellBExecTimeMs: 210,
          cellAQueueTimeMs: 5,
          cellBQueueTimeMs: 6,
          cellABytesProcessed: 2000,
          cellBBytesProcessed: 2100,
          cellALinesProcessed: 100,
          cellBLinesProcessed: 105,
          cellABytesPerSecond: 2000,
          cellBBytesPerSecond: 2100,
          cellALinesPerSecond: 100,
          cellBLinesPerSecond: 105,
          cellAEntriesReturned: 20,
          cellBEntriesReturned: 22,
          cellASplits: 2,
          cellBSplits: 2,
          cellAShards: 4,
          cellBShards: 4,
          cellAResponseHash: 'hash-server-a',
          cellBResponseHash: 'hash-server-b',
          cellAResponseSize: 1000,
          cellBResponseSize: 1100,
          cellAStatusCode: 200,
          cellBStatusCode: 200,
          cellATraceID: 'trace-server-a',
          cellBTraceID: 'trace-server-b',
          sampledAt: '2024-01-01T00:00:00Z',
          createdAt: '2024-01-01T00:00:00Z',
        },
      ];

      // Set up mock to return different data for the background request
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: mockQueries,
          total: 3,
          page: 1,
          pageSize: 10,
        })
        .mockResolvedValueOnce({
          queries: mockFilteredQueries,
          total: 1,
          page: 1,
          pageSize: 10,
        });

      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });

      // Click on Match filter
      fireEvent.click(screen.getByText('Match'));

      // Initially should show client-side filtered results
      expect(screen.getByText('Status: match')).toBeInTheDocument();
      expect(screen.queryByText('Status: mismatch')).not.toBeInTheDocument();

      // Eventually should show server-filtered data
      await waitFor(() => {
        expect(screen.getByText('server filtered query')).toBeInTheDocument();
      });

      // Should show the server-filtered query
      expect(screen.getByTestId('query-diff-view')).toHaveAttribute('data-correlation-id', 'server-filtered-match');
    });

    it('maintains client-side filtering when server data is not available', async () => {
      // Mock server to return error for background request
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: mockQueries,
          total: 3,
          page: 1,
          pageSize: 10,
        })
        .mockRejectedValueOnce(new Error('Server error'));

      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });

      // Click on Match filter
      fireEvent.click(screen.getByText('Match'));

      // Should still show client-side filtered results even if server request fails
      expect(screen.getByText('Status: match')).toBeInTheDocument();
      expect(screen.queryByText('Status: mismatch')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: error')).not.toBeInTheDocument();
    });
  });

  describe('Step 7: Smart Prefetching', () => {
    it('prefetches next page after current requests complete', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Should eventually prefetch next page
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_ALL);
      });
    });

    it('prefetches next page for current filter when background request completes', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Click on Match filter
      fireEvent.click(screen.getByText('Match'));

      // Should trigger background request
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH);
      });

      // Should eventually prefetch next page for the match filter
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_MATCH);
      });
    });

    it('does not prefetch when there is only one page', async () => {
      // Mock response with only one page
      mockFetchSampledQueries.mockResolvedValue({
        queries: [mockQueries[0]],
        total: 1,
        page: 1,
        pageSize: 10,
      });

      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Should not prefetch since there's only one page
      expect(mockFetchSampledQueries).not.toHaveBeenCalledWith(2, 10, OUTCOME_ALL);
    });
  });

  describe('Prefetch Cache Usage - TDD Step 1', () => {
    it('shows prefetched data immediately when navigating to next page', async () => {
      // Mock different data for page 1 vs page 2
      const page1Queries = [mockQueries[0]];
      const page2Queries = [mockQueries[1], mockQueries[2]];
      
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: page1Queries,
          total: 25,
          page: 1,
          pageSize: 10,
        })
        .mockResolvedValueOnce({
          queries: page2Queries,
          total: 25,
          page: 2,
          pageSize: 10,
        });

      renderGoldfishPage();

      // Wait for page 1 to load
      await waitFor(() => {
        expect(screen.getByTestId('query-diff-view')).toHaveAttribute('data-correlation-id', 'match-1');
      });

      // Wait for prefetch of page 2 to complete
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_ALL);
      });

      // Navigate to page 2
      fireEvent.click(screen.getByText('Next'));

      // Should show page 2 data immediately without loading state
      expect(screen.queryByText('Loading')).not.toBeInTheDocument();
      
      // Should show the page 2 data (mismatch-1 and error-1)
      const queryViews = screen.getAllByTestId('query-diff-view');
      expect(queryViews).toHaveLength(2);
      expect(queryViews[0]).toHaveAttribute('data-correlation-id', 'mismatch-1');
      expect(queryViews[1]).toHaveAttribute('data-correlation-id', 'error-1');
    });

    it('uses cached data when navigating to prefetched page and continues prefetching', async () => {
      // Mock different data for page 1 vs page 2
      const page1Queries = [mockQueries[0]];
      const page2Queries = [mockQueries[1], mockQueries[2]];
      
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: page1Queries,
          total: 25,
          page: 1,
          pageSize: 10,
        })
        .mockResolvedValueOnce({
          queries: page2Queries,
          total: 25,
          page: 2,
          pageSize: 10,
        });

      renderGoldfishPage();

      // Wait for page 1 to load and prefetch to complete
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_ALL);
      }, { timeout: 5000 });

      // Should have exactly 2 calls: initial load + prefetch
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);

      // Navigate to page 2
      fireEvent.click(screen.getByText('Next'));

      // Should show page 2 data immediately
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(2);
        expect(queryViews[0]).toHaveAttribute('data-correlation-id', 'mismatch-1');
      }, { timeout: 5000 });

      // Should have exactly 3 calls: initial load + prefetch page 2 + prefetch page 3
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(3);
      
      // Verify the calls are what we expect
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(1, 1, 10, OUTCOME_ALL); // Initial load
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(2, 2, 10, OUTCOME_ALL); // Prefetch page 2
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(3, 3, 10, OUTCOME_ALL); // Prefetch page 3
    }, 10000);

    it('waits for in-flight prefetch when navigating during prefetch', async () => {
      // Mock slow prefetch request
      const page1Queries = [mockQueries[0]];
      const page2Queries = [mockQueries[1], mockQueries[2]];
      
      let resolvePrefetch: (value: any) => void;
      const prefetchPromise = new Promise((resolve) => {
        resolvePrefetch = resolve;
      });

      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: page1Queries,
          total: 25,
          page: 1,
          pageSize: 10,
        })
        .mockImplementationOnce(() => prefetchPromise as any); // Slow prefetch

      renderGoldfishPage();

      // Wait for page 1 to load
      await waitFor(() => {
        expect(screen.getByTestId('query-diff-view')).toHaveAttribute('data-correlation-id', 'match-1');
      }, { timeout: 5000 });

      // Should have started prefetch for page 2
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);

      // Navigate to page 2 while prefetch is still in progress
      fireEvent.click(screen.getByText('Next'));

      // Should not trigger additional API calls - should wait for prefetch
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(2);

      // Complete the prefetch
      resolvePrefetch!({
        queries: page2Queries,
        total: 25,
        page: 2,
        pageSize: 10,
      });

      // Should show page 2 data once prefetch completes
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(2);
        expect(queryViews[0]).toHaveAttribute('data-correlation-id', 'mismatch-1');
      }, { timeout: 5000 });

      // Should have 3 calls: initial load + prefetch page 2 + prefetch page 3
      expect(mockFetchSampledQueries).toHaveBeenCalledTimes(3);
      
      // Verify the calls are what we expect
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(1, 1, 10, OUTCOME_ALL); // Initial load
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(2, 2, 10, OUTCOME_ALL); // Prefetch page 2
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(3, 3, 10, OUTCOME_ALL); // Prefetch page 3
    }, 10000);
  });
});