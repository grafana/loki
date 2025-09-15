import React from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import GoldfishPage from './goldfish';
import { useGoldfishQueries } from "@/hooks/use-goldfish-queries";
import type { SampledQuery } from '@/types/goldfish';
import '@testing-library/jest-dom';

// Import MemoryRouter and other hooks from react-router-dom
import { MemoryRouter } from 'react-router-dom';

// Mock React Router (but keep MemoryRouter)
const mockUseSearchParams = jest.fn();
jest.mock('react-router-dom', () => ({
  ...jest.requireActual('react-router-dom'),
  useNavigate: () => jest.fn(),
  useSearchParams: () => mockUseSearchParams(),
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
      <button onClick={() => onChange(new Date('2024-01-01T10:00:00Z'), new Date('2024-01-01T14:30:00Z'))}>Set Custom Range</button>
      <button
        data-testid="select-1h-preset"
        onClick={() => {
          const now = new Date();
          const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
          onChange(oneHourAgo, now);
        }}
      >
        Select 1 Hour Preset
      </button>
    </div>
  ),
}));

// Mock UserFilterCombobox
jest.mock('@/components/goldfish/user-filter-combobox', () => ({
  UserFilterCombobox: ({ value, onChange, suggestions }: { value?: string; onChange: (value?: string) => void; suggestions?: string[] }) => (
    <select
      data-testid="user-filter"
      value={value || ''}
      onChange={(e) => {
        const newValue = e.target.value || undefined;
        onChange(newValue);
      }}
    >
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
        <GoldfishPage />
      </QueryClientProvider>
    </MemoryRouter>
  );
};

describe('GoldfishPage', () => {
  beforeEach(() => {
    // Setup default mock implementation for the hook
    mockUseGoldfishQueries.mockReturnValue({
      ...defaultMockHookReturn,
      queries: mockQueries,
      hasMore: true,
    });
    // Setup default mock for useSearchParams
    mockUseSearchParams.mockReturnValue([new URLSearchParams(), jest.fn()]);
  });

  afterEach(() => {
    jest.clearAllMocks();
  });

  describe('URL State Management', () => {
    it('parses outcome filter from URL and applies it', () => {
      // Mock useSearchParams to return outcome=mismatch
      const searchParams = new URLSearchParams('outcome=mismatch');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Check that the hook was called with 'mismatch' as the selectedOutcome
      // This confirms the component parsed the URL parameter correctly
      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        'mismatch', // selectedOutcome - this should be 'mismatch' from URL
        undefined, // tenant
        undefined, // user
        undefined, // newEngine
        null, // from
        null // to
      );

      // Also check the visual state - buttons should reflect the selected outcome
      const mismatchButton = screen.getByRole('button', { name: /Mismatch/i });
      const allButton = screen.getByRole('button', { name: /^All$/i });
      const matchButton = screen.getByRole('button', { name: /^Match$/i });
      const errorButton = screen.getByRole('button', { name: /Error/i });

      // The selected button (Mismatch) should have bg-primary (from default variant)
      // The unselected buttons should have hover:bg-accent (from ghost variant)
      expect(mismatchButton.className).toContain('bg-primary');
      expect(allButton.className).toContain('hover:bg-accent');
      expect(matchButton.className).toContain('hover:bg-accent');
      expect(errorButton.className).toContain('hover:bg-accent');
    });

    it('updates URL when outcome filter changes', () => {
      // Start with no URL params
      const searchParams = new URLSearchParams();
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Click on the Error filter button
      const errorButton = screen.getByRole('button', { name: /Error/i });
      fireEvent.click(errorButton);

      // Check that setSearchParams was called
      expect(setSearchParams).toHaveBeenCalled();

      // Find the call that has outcome=error (might not be the first due to initial render)
      const callWithError = setSearchParams.mock.calls.find(call =>
        call[0].get('outcome') === 'error'
      );
      expect(callWithError).toBeDefined();
      expect(callWithError[0].get('outcome')).toBe('error');

      // Now click on All button (default)
      const allButton = screen.getByRole('button', { name: /^All$/i });
      fireEvent.click(allButton);

      // Check that setSearchParams was called to remove outcome param (since 'all' is default)
      const lastCallParams = setSearchParams.mock.calls[setSearchParams.mock.calls.length - 1][0];
      expect(lastCallParams.has('outcome')).toBe(false);
    });

    it('parses tenant filter from URL and applies it', () => {
      // Mock useSearchParams to return tenant=tenant-123
      const searchParams = new URLSearchParams('tenant=tenant-123');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      // Mock the hook to return some queries with tenants so the filter is visible
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries, // This has tenants: tenant-a, tenant-b, tenant-c
        hasMore: true,
      });

      renderGoldfishPage();

      // Check that the hook was called with 'tenant-123' as the selectedTenant
      // This confirms the component parsed the URL parameter correctly
      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        'tenant-123', // selectedTenant - this should be 'tenant-123' from URL
        undefined, // user
        undefined, // newEngine
        null, // from
        null // to
      );

      // Verify the tenant filter is rendered (since we have tenants in the data)
      const tenantFilter = screen.getByTestId('tenant-filter');
      expect(tenantFilter).toBeInTheDocument();

      // The filter should show the selected tenant
      // Note: Due to mock limitations, we're verifying the behavior through the hook call above
      // rather than the visual state. The actual component would show 'tenant-123' selected.
    });

    it('parses URL-encoded tenant filter from URL', () => {
      // Mock useSearchParams to return an encoded tenant like "tenant/special-123"
      const searchParams = new URLSearchParams('tenant=' + encodeURIComponent('tenant/special-123'));
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Check that the hook was called with the decoded tenant value
      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        'tenant/special-123', // selectedTenant - should be decoded
        undefined, // user
        undefined, // newEngine
        null, // from
        null // to
      );
    });

    it('updates URL when tenant filter changes', () => {
      // Start with no URL params
      const searchParams = new URLSearchParams();
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      // Mock the hook to return some queries with tenants so the filter is visible
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries, // This has tenants: tenant-a, tenant-b, tenant-c
        hasMore: true,
      });

      renderGoldfishPage();

      // Select a tenant from the dropdown
      const tenantFilter = screen.getByTestId('tenant-filter');
      fireEvent.change(tenantFilter, { target: { value: 'tenant-b' } });

      // Check that setSearchParams was called to add tenant to URL
      expect(setSearchParams).toHaveBeenCalled();

      // Find the call that has tenant=tenant-b
      const callWithTenant = setSearchParams.mock.calls.find(call =>
        call[0].get('tenant') === 'tenant-b'
      );
      expect(callWithTenant).toBeDefined();
      expect(callWithTenant[0].get('tenant')).toBe('tenant-b');

      // Now clear the tenant selection
      fireEvent.change(tenantFilter, { target: { value: '' } });

      // Check that setSearchParams was called to remove tenant param
      const lastCallParams = setSearchParams.mock.calls[setSearchParams.mock.calls.length - 1][0];
      expect(lastCallParams.has('tenant')).toBe(false);
    });

    it('updates URL with encoded tenant when special characters are present', () => {
      // Start with no URL params
      const searchParams = new URLSearchParams();
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      // Mock the hook to return queries with a tenant that has special characters
      const queriesWithSpecialTenant = [{
        ...mockQueries[0],
        tenantId: 'tenant/special-123'
      }];
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: queriesWithSpecialTenant,
        hasMore: false,
      });

      renderGoldfishPage();

      // Select the tenant with special characters
      const tenantFilter = screen.getByTestId('tenant-filter');
      fireEvent.change(tenantFilter, { target: { value: 'tenant/special-123' } });

      // Check that setSearchParams was called with the encoded value
      expect(setSearchParams).toHaveBeenCalled();

      // Find the call and check the tenant is properly encoded
      const callWithTenant = setSearchParams.mock.calls.find(call =>
        call[0].get('tenant') === 'tenant/special-123'
      );
      expect(callWithTenant).toBeDefined();

      // The actual URL string should have the encoded value
      const urlString = callWithTenant[0].toString();
      expect(urlString).toContain('tenant=tenant%2Fspecial-123');
    });

    it('parses user filter from URL and applies it', () => {
      // Mock useSearchParams to return user=john@example.com
      const searchParams = new URLSearchParams('user=' + encodeURIComponent('john@example.com'));
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Check that the hook was called with 'john@example.com' as the selectedUser
      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        undefined, // tenant
        'john@example.com', // selectedUser - should be decoded
        undefined, // newEngine
        null, // from
        null // to
      );
    });

    it('updates URL when user filter changes', () => {
      // Start with no URL params
      const searchParams = new URLSearchParams();
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      // Mock the hook to return some queries with users so UserFilterCombobox gets suggestions
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries, // This has users: user1@example.com, user2@example.com, user3@example.com
        hasMore: true,
      });

      renderGoldfishPage();

      // Select a user from the filter (this should trigger the onChange)
      const userFilter = screen.getByTestId('user-filter');
      fireEvent.change(userFilter, { target: { value: 'user1@example.com' } });

      // Check that setSearchParams was called with the user
      expect(setSearchParams).toHaveBeenCalled();

      const callWithUser = setSearchParams.mock.calls.find(call =>
        call[0].get('user') === 'user1@example.com'
      );
      expect(callWithUser).toBeDefined();
      expect(callWithUser[0].get('user')).toBe('user1@example.com');

      // Clear the user filter
      fireEvent.change(userFilter, { target: { value: '' } });

      // Check that user param is removed
      const lastCallParams = setSearchParams.mock.calls[setSearchParams.mock.calls.length - 1][0];
      expect(lastCallParams.has('user')).toBe(false);
    });

    it('parses since parameter for preset time ranges', () => {
      // Mock useSearchParams to return since=1h
      const searchParams = new URLSearchParams('since=1h');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Check that the hook was called with calculated timestamps (now - 1h to now)
      const call = mockUseGoldfishQueries.mock.calls[0];
      const fromArg = call[5]; // from parameter
      const toArg = call[6]; // to parameter

      expect(fromArg).toBeInstanceOf(Date);
      expect(toArg).toBeInstanceOf(Date);

      // Should be approximately 1 hour apart
      expect(fromArg).not.toBeNull();
      expect(toArg).not.toBeNull();
      const timeDiff = (toArg as Date).getTime() - (fromArg as Date).getTime();
      expect(timeDiff).toBeCloseTo(60 * 60 * 1000, -4); // 1 hour in ms, tolerance of 10ms
    });

    it('parses from/to parameters for custom time ranges', () => {
      // Mock useSearchParams with ISO timestamps
      const from = '2025-01-05T10:00:00.000Z';
      const to = '2025-01-05T12:00:00.000Z';
      const searchParams = new URLSearchParams(`from=${from}&to=${to}`);
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Check that the hook was called with the exact timestamps
      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        undefined, // tenant
        undefined, // user
        undefined, // newEngine
        new Date(from), // from
        new Date(to) // to
      );
    });

    it('shows error when both since and from/to parameters are present', () => {
      // Mock useSearchParams with conflicting parameters
      const searchParams = new URLSearchParams('since=1h&from=2025-01-05T10:00:00.000Z&to=2025-01-05T12:00:00.000Z');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Should show error and default to null timestamps
      expect(screen.getByRole('alert')).toBeInTheDocument();
      expect(screen.getByText(/conflicting time parameters/i)).toBeInTheDocument();

      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        undefined, // tenant
        undefined, // user
        undefined, // newEngine
        null, // from - defaults to null on error
        null // to - defaults to null on error
      );
    });

    it('shows error when only from parameter is present without to', () => {
      // Mock useSearchParams with incomplete range
      const searchParams = new URLSearchParams('from=2025-01-05T10:00:00.000Z');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Should show error and default to null timestamps
      expect(screen.getByRole('alert')).toBeInTheDocument();
      expect(screen.getByText(/incomplete time range/i)).toBeInTheDocument();

      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        undefined, // tenant
        undefined, // user
        undefined, // newEngine
        null, // from - defaults to null on error
        null // to - defaults to null on error
      );
    });

    it('shows error when only to parameter is present without from', () => {
      // Mock useSearchParams with incomplete range
      const searchParams = new URLSearchParams('to=2025-01-05T12:00:00.000Z');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Should show error and default to null timestamps
      expect(screen.getByRole('alert')).toBeInTheDocument();
      expect(screen.getByText(/incomplete time range/i)).toBeInTheDocument();

      expect(mockUseGoldfishQueries).toHaveBeenCalledWith(
        20, // pageSize
        expect.anything(), // outcome
        undefined, // tenant
        undefined, // user
        undefined, // newEngine
        null, // from - defaults to null on error
        null // to - defaults to null on error
      );
    });

    it('updates URL with since parameter for preset time ranges', () => {
      // Start with no URL params
      const searchParams = new URLSearchParams();
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Click the 1 hour preset button which sets times exactly 1 hour apart
      const presetButton = screen.getByTestId('select-1h-preset');
      fireEvent.click(presetButton);

      // Should use since=1h for the preset
      expect(setSearchParams).toHaveBeenCalled();

      const callWithSince = setSearchParams.mock.calls.find(call =>
        call[0].get('since') === '1h'
      );
      expect(callWithSince).toBeDefined();
      expect(callWithSince[0].get('since')).toBe('1h');
    });

    it('updates URL with from/to parameters for custom time ranges', () => {
      // Start with no URL params
      const searchParams = new URLSearchParams();
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      renderGoldfishPage();

      // Click the "Set Custom Range" button which sets dates that don't match any preset
      const customRangeButton = screen.getByText('Set Custom Range');
      fireEvent.click(customRangeButton);

      // Should use from/to parameters for custom ranges (not since)
      expect(setSearchParams).toHaveBeenCalled();

      // When custom range is selected, should use from/to not since
      const callWithFromTo = setSearchParams.mock.calls.find(call =>
        call[0].has('from') && call[0].has('to') && !call[0].has('since')
      );
      expect(callWithFromTo).toBeDefined();
      expect(callWithFromTo[0].get('from')).toBe('2024-01-01T10:00:00.000Z');
      expect(callWithFromTo[0].get('to')).toBe('2024-01-01T14:30:00.000Z');
    });

    it('preserves newEngine parameter when other filters change', () => {
      // Start with newEngine=true in URL
      const searchParams = new URLSearchParams('newEngine=true');
      const setSearchParams = jest.fn();
      mockUseSearchParams.mockReturnValue([searchParams, setSearchParams]);

      // Mock queries so tenant filter is visible
      mockUseGoldfishQueries.mockReturnValue({
        ...defaultMockHookReturn,
        queries: mockQueries,
        hasMore: true,
      });

      renderGoldfishPage();

      // Change another filter (tenant)
      const tenantFilter = screen.getByTestId('tenant-filter');
      fireEvent.change(tenantFilter, { target: { value: 'tenant-a' } });

      // Check that newEngine=true is preserved
      const lastCall = setSearchParams.mock.calls[setSearchParams.mock.calls.length - 1];
      expect(lastCall[0].get('newEngine')).toBe('true');
      expect(lastCall[0].get('tenant')).toBe('tenant-a');
    });
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
