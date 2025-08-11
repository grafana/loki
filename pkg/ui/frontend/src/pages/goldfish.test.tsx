import React, { useState, useMemo } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import GoldfishPage from './goldfish';
import * as goldfishApi from '@/lib/goldfish-api';
import { OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR, OutcomeFilter, SampledQuery } from '@/types/goldfish';
import { useGoldfishQueries } from "@/hooks/use-goldfish-queries";
import { QueryDiffView } from "@/components/goldfish/query-diff-view";
import { filterQueriesByOutcome } from "@/lib/goldfish-utils";
import '@testing-library/jest-dom';

// Mock the goldfish API
jest.mock('@/lib/goldfish-api');
const mockFetchSampledQueries = goldfishApi.fetchSampledQueries as jest.MockedFunction<typeof goldfishApi.fetchSampledQueries>;

// Import MemoryRouter from react-router-dom
import { MemoryRouter } from 'react-router-dom';

// Mock React Router (but keep MemoryRouter)
jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useNavigate: () => jest.fn(),
  useSearchParams: () => [new URLSearchParams(), jest.fn()],
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
      {query.user && <div>{query.user}</div>}
    </div>
  ),
}));

const mockQueries = [
  {
    correlationId: 'match-1',
    tenantId: 'tenant-a',
    user: 'unknown',
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
    user: 'unknown',
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
    user: 'unknown',
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

  describe('User Display', () => {
    it('displays user information in the query list', async () => {
      const queriesWithUser = [
        {
          ...mockQueries[0],
          user: 'john.doe@example.com',
        },
        {
          ...mockQueries[1],
          user: 'jane.smith@example.com',
        },
      ];

      mockFetchSampledQueries.mockResolvedValue({
        queries: queriesWithUser,
        total: 2,
        page: 1,
        pageSize: 10,
      });

      renderGoldfishPage();

      // Wait for data to load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(2);
      });

      // Check that user information is displayed
      expect(screen.getByText('john.doe@example.com')).toBeInTheDocument();
      expect(screen.getByText('jane.smith@example.com')).toBeInTheDocument();
    });
  });

  describe('Step 3: Immediate Filter Response', () => {
    it('shows filtered results immediately when filter is clicked', async () => {
      // Mock different responses for main query and match query
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: mockQueries,
          total: 3,
          page: 1,
          pageSize: 10,
        })
        .mockResolvedValueOnce({
          queries: [mockQueries[0]], // Only match query
          total: 1,
          page: 1,
          pageSize: 10,
        });

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

      // Should show filtered results (initially from client-side fallback, then from server)
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(1);
        expect(screen.getByText('Status: match')).toBeInTheDocument();
      });
      expect(screen.queryByText('Status: mismatch')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: error')).not.toBeInTheDocument();
    });

    it('shows different filtered results when switching between filters', async () => {
      // Mock different responses for different outcome filters
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: mockQueries,
          total: 3,
          page: 1,
          pageSize: 10,
        })
        .mockResolvedValueOnce({
          queries: [mockQueries[1]], // Only mismatch query
          total: 1,
          page: 1,
          pageSize: 10,
        })
        .mockResolvedValueOnce({
          queries: [mockQueries[2]], // Only error query
          total: 1,
          page: 1,
          pageSize: 10,
        });

      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });

      // Click on Mismatch filter
      fireEvent.click(screen.getByText('Mismatch'));

      // Should show only mismatch results
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(1);
        expect(screen.getByText('Status: mismatch')).toBeInTheDocument();
      });
      expect(screen.queryByText('Status: match')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: error')).not.toBeInTheDocument();

      // Click on Error filter
      fireEvent.click(screen.getByText('Error'));

      // Should show only error results
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(1);
        expect(screen.getByText('Status: error')).toBeInTheDocument();
      });
      expect(screen.queryByText('Status: match')).not.toBeInTheDocument();
      expect(screen.queryByText('Status: mismatch')).not.toBeInTheDocument();
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

      // Should wait for background requests to be triggered
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH, undefined, undefined, undefined);
      });

      // Primary query should have been called with OUTCOME_ALL first
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL, undefined, undefined, undefined);
      
      // Background queries should also be called when specific filters are selected
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH, undefined, undefined, undefined);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MISMATCH, undefined, undefined, undefined);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ERROR, undefined, undefined, undefined);
    });

    it('fetches data with OUTCOME_ALL regardless of selected filter', async () => {
      renderGoldfishPage();

      // Wait for initial load
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledTimes(1);
      });

      // Verify it was called with OUTCOME_ALL
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL, undefined, undefined, undefined);
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH, undefined, undefined, undefined);
      });

      // Should have multiple calls (initial + background + prefetch)
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH, undefined, undefined, undefined);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL, undefined, undefined, undefined);
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MISMATCH, undefined, undefined, undefined);
      });

      // Click on Error filter
      fireEvent.click(screen.getByText('Error'));

      // Should trigger background request with error outcome
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ERROR, undefined, undefined, undefined);
      });

      // Should have called with different outcomes
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL, undefined, undefined, undefined);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MISMATCH, undefined, undefined, undefined);
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ERROR, undefined, undefined, undefined);
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH, undefined, undefined, undefined);
      });

      // Click on All filter
      fireEvent.click(screen.getByText('All'));

      // Should not trigger additional background request for "All"
      // because we already have all data
      expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_ALL, undefined, undefined, undefined);
      
      // Should not make additional calls for "All" outcome background query
      // (but prefetch calls may still happen)
      const allCalls = mockFetchSampledQueries.mock.calls;
      const backgroundAllCalls = allCalls.filter(call => 
        call[0] === 1 && call[1] === 10 && call[2] === OUTCOME_ALL && call[3] === undefined && call[4] === undefined && call[5] === undefined
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
          user: 'unknown',
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

      // Eventually should show server-filtered data
      await waitFor(() => {
        expect(screen.getByText('server filtered query')).toBeInTheDocument();
      });

      // Should show the server-filtered query
      expect(screen.getByTestId('query-diff-view')).toHaveAttribute('data-correlation-id', 'server-filtered-match');
    });

    it('shows previous results when server request fails', async () => {
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

      // Should still show the original unfiltered results since server request failed
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_ALL, undefined, undefined, undefined);
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(1, 10, OUTCOME_MATCH, undefined, undefined, undefined);
      });

      // Should eventually prefetch next page for the match filter
      await waitFor(() => {
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_MATCH, undefined, undefined, undefined);
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_ALL, undefined, undefined, undefined);
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
        expect(mockFetchSampledQueries).toHaveBeenCalledWith(2, 10, OUTCOME_ALL, undefined, undefined, undefined);
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
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(1, 1, 10, OUTCOME_ALL, undefined, undefined, undefined); // Initial load
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(2, 2, 10, OUTCOME_ALL, undefined, undefined, undefined); // Prefetch page 2
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(3, 3, 10, OUTCOME_ALL, undefined, undefined, undefined); // Prefetch page 3
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
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(1, 1, 10, OUTCOME_ALL, undefined, undefined, undefined); // Initial load
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(2, 2, 10, OUTCOME_ALL, undefined, undefined, undefined); // Prefetch page 2
      expect(mockFetchSampledQueries).toHaveBeenNthCalledWith(3, 3, 10, OUTCOME_ALL, undefined, undefined, undefined); // Prefetch page 3
    }, 10000);
  });

  describe('User Filtering', () => {
    it('filters queries by user when user filter is selected', async () => {
      const queriesWithDifferentUsers = [
        {
          ...mockQueries[0],
          user: 'john.doe@example.com',
        },
        {
          ...mockQueries[1], 
          user: 'jane.smith@example.com',
        },
        {
          ...mockQueries[2],
          user: 'john.doe@example.com',
        },
      ];

      mockFetchSampledQueries.mockResolvedValue({
        queries: queriesWithDifferentUsers,
        total: 3,
        page: 1,
        pageSize: 10,
      });

      const { rerender } = renderGoldfishPage();

      // Wait for data to load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      });

      // Check that all users are displayed initially
      expect(screen.getAllByText('john.doe@example.com')).toHaveLength(2); // 2 queries with this user
      expect(screen.getByText('jane.smith@example.com')).toBeInTheDocument();

      // Test user filtering functionality by creating a new component instance with selectedUser state
      // This tests the filtering logic without needing to interact with the Select component
      const GoldfishPageWithUserFilter = () => {
        const [selectedUser, setSelectedUser] = useState('john.doe@example.com');
        const [selectedTenant, setSelectedTenant] = useState<string>("all");
        const [selectedOutcome, setSelectedOutcome] = useState<OutcomeFilter>(OUTCOME_ALL);
        const [page, setPage] = useState(1);
        const pageSize = 10;
        
        const { data, isLoading, error, refetch, totalPages } = useGoldfishQueries(page, pageSize, selectedOutcome);
        const allQueries = useMemo(() => (data as { queries: SampledQuery[] })?.queries || [], [data]);
        
        // Extract unique users from queries
        const uniqueUsers = useMemo(() => {
          const users = new Set(allQueries.map((q) => q.user).filter((u) => u && u !== "unknown"));
          return Array.from(users).sort();
        }, [allQueries]);
        
        // Apply client-side filtering based on tenant, user, and outcome
        const filteredQueries = useMemo(() => {
          const outcomeFiltered = filterQueriesByOutcome(allQueries, selectedOutcome);
          return outcomeFiltered.filter(query => {
            const matchesTenant = selectedTenant === "all" || query.tenantId === selectedTenant;
            const matchesUser = selectedUser === "all" || query.user === selectedUser;
            return matchesTenant && matchesUser;
          });
        }, [allQueries, selectedTenant, selectedUser, selectedOutcome]);

        return (
          <div data-testid="page-container">
            <div data-testid="filtered-queries">
              {filteredQueries.map((query) => (
                <QueryDiffView key={query.correlationId} query={query} />
              ))}
            </div>
          </div>
        );
      };

      // Unmount the original component and render with user filter
      rerender(
        <QueryClientProvider client={new QueryClient()}>
          <GoldfishPageWithUserFilter />
        </QueryClientProvider>
      );

      // Wait for filtering to take effect
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(2); // Should only show john.doe's queries
      });
      
      // Check that only john.doe's queries are visible
      const userBadges = screen.getAllByText('john.doe@example.com');
      expect(userBadges).toHaveLength(2);
      
      // jane.smith should not be visible
      expect(screen.queryByText('jane.smith@example.com')).not.toBeInTheDocument();
    });

    it('shows all queries when "All Users" is selected', async () => {
      const queriesWithDifferentUsers = [
        {
          ...mockQueries[0],
          user: 'john.doe@example.com',
        },
        {
          ...mockQueries[1],
          user: 'jane.smith@example.com', 
        },
      ];

      mockFetchSampledQueries.mockResolvedValue({
        queries: queriesWithDifferentUsers,
        total: 2,
        page: 1,
        pageSize: 10,
      });

      // Test "All Users" filtering functionality by directly testing the filtering logic
      const GoldfishPageWithAllUsersFilter = () => {
        const [selectedUser, setSelectedUser] = useState('all'); // "all" means show all users
        const [selectedTenant, setSelectedTenant] = useState<string>("all");
        const [selectedOutcome, setSelectedOutcome] = useState<OutcomeFilter>(OUTCOME_ALL);
        const [page, setPage] = useState(1);
        const pageSize = 10;
        
        const { data, isLoading, error, refetch, totalPages } = useGoldfishQueries(page, pageSize, selectedOutcome);
        const allQueries = useMemo(() => (data as { queries: SampledQuery[] })?.queries || [], [data]);
        
        // Apply client-side filtering based on tenant, user, and outcome
        const filteredQueries = useMemo(() => {
          const outcomeFiltered = filterQueriesByOutcome(allQueries, selectedOutcome);
          return outcomeFiltered.filter(query => {
            const matchesTenant = selectedTenant === "all" || query.tenantId === selectedTenant;
            const matchesUser = selectedUser === "all" || query.user === selectedUser;
            return matchesTenant && matchesUser;
          });
        }, [allQueries, selectedTenant, selectedUser, selectedOutcome]);

        return (
          <div data-testid="all-users-page-container">
            <div data-testid="all-users-filtered-queries">
              {filteredQueries.map((query) => (
                <QueryDiffView key={query.correlationId} query={query} />
              ))}
            </div>
          </div>
        );
      };

      // Render the "All Users" filter component directly (without initial page render)
      render(<GoldfishPageWithAllUsersFilter />, { 
        wrapper: ({ children }) => (
          <QueryClientProvider client={new QueryClient()}>
            {children}
          </QueryClientProvider>
        )
      });

      // Should show all queries
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(2);
      });
      expect(screen.getByText('john.doe@example.com')).toBeInTheDocument();
      expect(screen.getByText('jane.smith@example.com')).toBeInTheDocument();
    });
  });

  describe('New Engine Filtering', () => {
    it('filters queries to show only those using new engine', async () => {
      const queriesWithEngineInfo = [
        {
          ...mockQueries[0],
          correlationId: 'new-engine-1',
          cellAUsedNewEngine: true,
          cellBUsedNewEngine: false,
        },
        {
          ...mockQueries[1],
          correlationId: 'new-engine-2',
          cellAUsedNewEngine: false,
          cellBUsedNewEngine: true,
        },
        {
          ...mockQueries[0],
          correlationId: 'legacy-only-1',
          cellAUsedNewEngine: false,
          cellBUsedNewEngine: false,
        },
        {
          ...mockQueries[1],
          correlationId: 'legacy-only-2',
          cellAUsedNewEngine: false,
          cellBUsedNewEngine: false,
        },
      ];

      // Mock initial load (no filter)
      mockFetchSampledQueries
        .mockResolvedValueOnce({
          queries: queriesWithEngineInfo,
          total: 4,
          page: 1,
          pageSize: 10,
        })
        // Mock response when new engine filter is applied
        .mockResolvedValueOnce({
          queries: [queriesWithEngineInfo[0], queriesWithEngineInfo[1]], // Only new engine queries
          total: 2,
          page: 1,
          pageSize: 10,
        });

      render(<GoldfishPage />, {
        wrapper: ({ children }) => (
          <QueryClientProvider client={new QueryClient()}>
            {children}
          </QueryClientProvider>
        ),
      });

      // Wait for initial load
      await waitFor(() => {
        expect(screen.getAllByTestId('query-diff-view')).toHaveLength(4);
      });

      // Find and toggle the "New Engine Only" checkbox
      const newEngineCheckbox = screen.getByRole('checkbox', { name: /new engine only/i });
      expect(newEngineCheckbox).not.toBeChecked();

      // Toggle the checkbox
      fireEvent.click(newEngineCheckbox);

      // Should now only show queries that used new engine in at least one cell
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(2);
        expect(queryViews[0]).toHaveAttribute('data-correlation-id', 'new-engine-1');
        expect(queryViews[1]).toHaveAttribute('data-correlation-id', 'new-engine-2');
      });

      // Verify legacy-only queries are not shown
      expect(screen.queryByText('[data-correlation-id="legacy-only-1"]')).not.toBeInTheDocument();
      expect(screen.queryByText('[data-correlation-id="legacy-only-2"]')).not.toBeInTheDocument();
    });

    it('persists new engine filter in URL params', async () => {
      const queriesWithEngineInfo = [
        {
          ...mockQueries[0],
          correlationId: 'new-engine-1',
          cellAUsedNewEngine: true,
          cellBUsedNewEngine: false,
        },
        {
          ...mockQueries[1],
          correlationId: 'legacy-only-1',
          cellAUsedNewEngine: false,
          cellBUsedNewEngine: false,
        },
      ];

      // Mock response with new engine filter applied (URL param is true)
      mockFetchSampledQueries.mockResolvedValue({
        queries: [queriesWithEngineInfo[0]], // Only new engine query
        total: 1,
        page: 1,
        pageSize: 10,
      });

      // For this test, temporarily override the useSearchParams mock
      const originalMock = (require('react-router-dom') as any).useSearchParams;
      (require('react-router-dom') as any).useSearchParams = jest.requireActual('react-router-dom').useSearchParams;

      // Render with newEngine=true in URL
      render(<GoldfishPage />, {
        wrapper: ({ children }) => (
          <QueryClientProvider client={new QueryClient()}>
            <MemoryRouter initialEntries={['/?newEngine=true']}>
              {children}
            </MemoryRouter>
          </QueryClientProvider>
        ),
      });

      // Wait for initial load
      await waitFor(() => {
        const checkbox = screen.getByRole('checkbox', { name: /new engine only/i });
        expect(checkbox).toBeChecked();
      });

      // Should only show new engine queries based on URL param
      await waitFor(() => {
        const queryViews = screen.getAllByTestId('query-diff-view');
        expect(queryViews).toHaveLength(1);
        expect(queryViews[0]).toHaveAttribute('data-correlation-id', 'new-engine-1');
      });

      // Toggle the checkbox off
      const checkbox = screen.getByRole('checkbox', { name: /new engine only/i });
      fireEvent.click(checkbox);

      // Verify URL would be updated (can't test actual URL update in unit test)
      await waitFor(() => {
        expect(checkbox).not.toBeChecked();
      });

      // Restore the original mock
      (require('react-router-dom') as any).useSearchParams = originalMock;
    });
  });
});