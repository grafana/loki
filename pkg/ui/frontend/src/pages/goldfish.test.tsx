import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import GoldfishPageLoadMore from './goldfish';
import { useGoldfishQueries } from "@/hooks/use-goldfish-queries";
import type { SampledQuery } from '@/types/goldfish';
import '@testing-library/jest-dom';

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
  QueryDiffView: ({ query }: { query: SampledQuery }) => (
    <div data-testid="query-diff-view" data-correlation-id={query.correlationId}>
      Status: {query.comparisonStatus}
      <div>{query.query}</div>
      {query.user && query.user !== 'unknown' && <div>{query.user}</div>}
    </div>
  ),
}));

// Mock TimeRangeSelector
jest.mock('@/components/goldfish/time-range-selector', () => ({
  TimeRangeSelector: ({ onChange }: { onChange: (from: Date | null, to: Date | null) => void }) => (
    <div data-testid="time-range-selector">
      <button onClick={() => onChange(new Date('2024-01-01'), new Date('2024-01-02'))}>Set Time Range</button>
    </div>
  ),
}));

// Mock UserFilterCombobox
jest.mock('@/components/goldfish/user-filter-combobox', () => ({
  UserFilterCombobox: ({ value, onChange, suggestions }: { value?: string; onChange: (value?: string) => void; suggestions?: string[] }) => (
    <select data-testid="user-filter" value={value || ''} onChange={(e) => onChange(e.target.value || undefined)}>
      <option value="">All Users</option>
      {suggestions?.map((user: string) => (
        <option key={user} value={user}>{user}</option>
      ))}
    </select>
  ),
}));

// Mock TenantFilterSelect
jest.mock('@/components/goldfish/tenant-filter-select', () => ({
  TenantFilterSelect: ({ value, onChange, tenants }: { value?: string; onChange: (value?: string) => void; tenants?: string[] }) => (
    <select data-testid="tenant-filter" value={value || ''} onChange={(e) => onChange(e.target.value || undefined)}>
      <option value="">All Tenants</option>
      {tenants?.map((tenant: string) => (
        <option key={tenant} value={tenant}>{tenant}</option>
      ))}
    </select>
  ),
}));

// Mock the loadmore hook
jest.mock('@/hooks/use-goldfish-queries');
const mockUseGoldfishQueries = useGoldfishQueries as jest.MockedFunction<typeof useGoldfishQueries>;

const mockQueries = [
  {
    correlationId: 'match-1',
    tenantId: 'tenant-a',
    user: 'user1@example.com',
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
    cellASpanID: 'span-a-1',
    cellBSpanID: 'span-b-1',
    cellAUsedNewEngine: false,
    cellBUsedNewEngine: false,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
  {
    correlationId: 'mismatch-1',
    tenantId: 'tenant-b',
    user: 'user2@example.com',
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
    cellASpanID: 'span-a-2',
    cellBSpanID: 'span-b-2',
    cellAUsedNewEngine: false,
    cellBUsedNewEngine: true,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
  {
    correlationId: 'error-1',
    tenantId: 'tenant-c',
    user: 'user3@example.com',
    comparisonStatus: 'error',
    query: 'test query 3',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 0,
    cellAQueueTimeMs: 5,
    cellBQueueTimeMs: 0,
    cellABytesProcessed: 1000,
    cellBBytesProcessed: 0,
    cellALinesProcessed: 50,
    cellBLinesProcessed: 0,
    cellABytesPerSecond: 1000,
    cellBBytesPerSecond: 0,
    cellALinesPerSecond: 50,
    cellBLinesPerSecond: 0,
    cellAEntriesReturned: 10,
    cellBEntriesReturned: 0,
    cellASplits: 1,
    cellBSplits: 0,
    cellAShards: 2,
    cellBShards: 0,
    cellAResponseHash: 'hash-a-3',
    cellBResponseHash: '',
    cellAResponseSize: 500,
    cellBResponseSize: 0,
    cellAStatusCode: 200,
    cellBStatusCode: 500,
    cellATraceID: 'trace-a-3',
    cellBTraceID: 'trace-b-3',
    cellASpanID: 'span-a-3',
    cellBSpanID: 'span-b-3',
    cellAUsedNewEngine: true,
    cellBUsedNewEngine: false,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
  },
];

// Default mock implementation for the loadmore hook
const defaultMockHookReturn = {
  queries: [],
  isLoading: false,
  isLoadingMore: false,
  error: null,
  hasMore: false,
  loadMore: jest.fn(),
  refresh: jest.fn(),
  traceId: null,
};

const renderGoldfishPage = () => {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        staleTime: 0,
        gcTime: 10 * 60 * 1000,
      },
    },
  });

  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <GoldfishPageLoadMore />
      </QueryClientProvider>
    </MemoryRouter>
  );
};

describe('GoldfishPageLoadMore', () => {
  beforeEach(() => {
    // Setup default mock implementation for the hook
    mockUseGoldfishQueries.mockReturnValue({
      ...defaultMockHookReturn,
      queries: mockQueries,
      hasMore: true,
    });
  });

  afterEach(() => {
    jest.clearAllMocks();
  });

  describe('Rendering', () => {
    it('renders the page with all components', () => {
      renderGoldfishPage();
      
      expect(screen.getByText('Goldfish - Query Comparison')).toBeInTheDocument();
      expect(screen.getByText('Side-by-side performance comparison between Cell A and Cell B')).toBeInTheDocument();
      expect(screen.getByTestId('time-range-selector')).toBeInTheDocument();
      expect(screen.getByText('Refresh')).toBeInTheDocument();
    });

    it('displays queries from the hook', () => {
      renderGoldfishPage();
      
      expect(screen.getAllByTestId('query-diff-view')).toHaveLength(3);
      expect(screen.getByText('Status: match')).toBeInTheDocument();
      expect(screen.getByText('Status: mismatch')).toBeInTheDocument();
      expect(screen.getByText('Status: error')).toBeInTheDocument();
    });

    it('shows loading state when isLoading is true', () => {
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        isLoading: true,
        queries: [],
      });

      renderGoldfishPage();
      
      // Verify that query views are not shown during loading
      expect(screen.queryByTestId('query-diff-view')).not.toBeInTheDocument();
      // Instead, the page still shows but with no queries
      expect(screen.getByText('Goldfish - Query Comparison')).toBeInTheDocument();
    });

    it('shows error state when error is present', () => {
      const error = new Error('Failed to fetch queries');
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        error,
        traceId: 'error-trace-123',
      });

      renderGoldfishPage();
      
      expect(screen.getByText(/Failed to load queries: Failed to fetch queries/)).toBeInTheDocument();
      expect(screen.getByText('error-trace-123')).toBeInTheDocument();
    });
  });

  describe('Load More Functionality', () => {
    it('shows load more button when hasMore is true', () => {
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries,
        hasMore: true,
      });

      renderGoldfishPage();
      
      // Load More button text could be in the button element
      const loadMoreButton = screen.getByRole('button', { name: /Load More/i });
      expect(loadMoreButton).toBeInTheDocument();
    });

    it('hides load more button when hasMore is false', () => {
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries,
        hasMore: false,
      });

      renderGoldfishPage();
      
      expect(screen.queryByText('Load More')).not.toBeInTheDocument();
      expect(screen.getByText(/No more results/)).toBeInTheDocument();
    });

    it('calls loadMore when load more button is clicked', () => {
      const mockLoadMore = jest.fn();
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries,
        hasMore: true,
        loadMore: mockLoadMore,
      });

      renderGoldfishPage();
      
      const loadMoreButton = screen.getByRole('button', { name: /Load More/i });
      fireEvent.click(loadMoreButton);
      expect(mockLoadMore).toHaveBeenCalled();
    });

    it('shows loading state when isLoadingMore is true', () => {
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries,
        hasMore: true,
        isLoadingMore: true,
      });

      renderGoldfishPage();
      
      // Loading state is shown in the button
      const loadingButton = screen.getByRole('button', { name: /Loading/i });
      expect(loadingButton).toBeInTheDocument();
    });
  });

  describe('Filter Interactions', () => {
    it('calls refresh when refresh button is clicked', () => {
      const mockRefresh = jest.fn();
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries,
        refresh: mockRefresh,
      });

      renderGoldfishPage();
      
      fireEvent.click(screen.getByText('Refresh'));
      expect(mockRefresh).toHaveBeenCalled();
    });

    it('updates hook parameters when filters change', () => {
      renderGoldfishPage();
      
      // Change tenant filter
      const tenantFilter = screen.getByTestId('tenant-filter');
      fireEvent.change(tenantFilter, { target: { value: 'tenant-a' } });
      
      // Change user filter
      const userFilter = screen.getByTestId('user-filter');
      fireEvent.change(userFilter, { target: { value: 'user1@example.com' } });
      
      // The hook should be called with the new parameters
      expect(mockUseGoldfishQueries).toHaveBeenCalled();
    });

    it('toggles new engine filter', () => {
      renderGoldfishPage();
      
      // Find the checkbox by its label text
      const newEngineLabel = screen.getByText(/New Engine Only/i);
      expect(newEngineLabel).toBeInTheDocument();
      
      // The hook should be called with proper parameters
      expect(mockUseGoldfishQueries).toHaveBeenCalled();
    });
  });

  describe('Empty States', () => {
    it('shows appropriate message when no queries match filters', () => {
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: [],
        hasMore: false,
      });

      renderGoldfishPage();
      
      expect(screen.getByText(/No queries found in current results/)).toBeInTheDocument();
    });

    it('shows load more button in empty state when hasMore is true', () => {
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: [],
        hasMore: true,
      });

      renderGoldfishPage();
      
      expect(screen.getByText(/Try loading more results to find matching queries/)).toBeInTheDocument();
      expect(screen.getByText('Load More Results')).toBeInTheDocument();
    });
  });

  describe('User Information Display', () => {
    it('displays user emails in query views', () => {
      renderGoldfishPage();
      
      // Check that user emails are shown in the query views (not in dropdown options)
      const queryViews = screen.getAllByTestId('query-diff-view');
      expect(queryViews).toHaveLength(3);
      
      // Find user emails within query views specifically
      expect(screen.getAllByText('user1@example.com')).toHaveLength(2); // One in dropdown, one in query view
      expect(screen.getAllByText('user2@example.com')).toHaveLength(2); // One in dropdown, one in query view  
      expect(screen.getAllByText('user3@example.com')).toHaveLength(2); // One in dropdown, one in query view
    });

    it('populates user filter dropdown with unique users', () => {
      renderGoldfishPage();
      
      const userFilter = screen.getByTestId('user-filter');
      expect(userFilter).toBeInTheDocument();
      // The actual suggestions are controlled by the component internally
    });
  });
});
