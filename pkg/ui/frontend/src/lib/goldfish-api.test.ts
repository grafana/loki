import { fetchSampledQueries } from './goldfish-api';
import { absolutePath } from '../util';

const mockAbsolutePath = absolutePath as jest.MockedFunction<typeof absolutePath>;

// Mock the util module to spy on absolutePath calls
jest.mock('../util', () => ({
  absolutePath: jest.fn(),
}));

// Mock tracing module to avoid random trace IDs in tests
jest.mock('./tracing', () => ({
  createTraceContext: jest.fn(() => ({
    traceId: 'test-trace-id-123',
    spanId: 'test-span-id-456',
  })),
  createTraceHeaders: jest.fn((traceId: string, spanId: string) => ({
    'X-Trace-Id': traceId,
    'X-Span-Id': spanId,
    'traceparent': `00-${traceId}-${spanId}-01`,
  })),
  extractTraceId: jest.fn(() => 'test-trace-id-123'),
}));

// Mock fetch globally
const mockFetch = jest.fn();
global.fetch = mockFetch;

// Mock window.location for testing
const mockLocation = (pathname: string) => {
  delete (window as unknown as { location?: Location }).location;
  (window as unknown as { location: { pathname: string } }).location = { pathname };
};

// Helper function to create standard expected headers
const expectedHeaders = {
  headers: {
    'X-Trace-Id': 'test-trace-id-123',
    'X-Span-Id': 'test-span-id-456',
    'traceparent': '00-test-trace-id-123-test-span-id-456-01',
  },
};

// Helper function to create mock response
const mockResponse = (data: unknown = { queries: [], hasMore: false, page: 1, pageSize: 20 }) => ({
  ok: true,
  json: async () => data,
  headers: {
    get: jest.fn(() => null),
  },
});

describe('goldfish-api', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockLocation('/ui/');
  });

  describe('fetchSampledQueries API URL construction', () => {
    it('uses absolutePath to construct API URL for local development', async () => {
      // Setup: Mock absolutePath to return local development path
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call the API function
      await fetchSampledQueries(1, 20);

      // Assert: Verify absolutePath was called with correct relative path
      expect(mockAbsolutePath).toHaveBeenCalledWith('/api/v1/goldfish/queries');
      
      // Assert: Verify fetch was called with the constructed URL and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20', expectedHeaders);
    });

    it('preserves query parameters when using absolutePath', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/base/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse({
        queries: [],
        hasMore: true,
        page: 2,
        pageSize: 15,
      }));

      // Act: Call with specific parameters
      await fetchSampledQueries(2, 15);

      // Assert: Verify the complete URL with all query parameters and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/base/api/v1/goldfish/queries?page=2&pageSize=15', expectedHeaders);
    });


    it('handles API errors correctly', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock API error response
      mockFetch.mockResolvedValueOnce({
        ok: false,
        statusText: 'Internal Server Error',
        text: async () => '',
        headers: {
          get: jest.fn(() => null),
        },
      });

      // Act & Assert: Verify function returns error instead of throwing
      const result = await fetchSampledQueries(1, 20);
      expect(result.error).toBeDefined();
      expect(result.error?.message).toContain('Failed to fetch sampled queries: Internal Server Error');
      expect(result.traceId).toBe('test-trace-id-123');
      
      // Assert: Verify absolutePath was still called
      expect(mockAbsolutePath).toHaveBeenCalledWith('/api/v1/goldfish/queries');
    });
  });

  describe('filter parameters', () => {
    it('includes tenant filter in query parameters', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with tenant filter
      await fetchSampledQueries(1, 20, 'tenant-123');

      // Assert: Verify URL includes tenant parameter and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20&tenant=tenant-123', expectedHeaders);
    });

    it('includes user filter in query parameters', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with user filter
      await fetchSampledQueries(1, 20, undefined, 'alice');

      // Assert: Verify URL includes user parameter and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20&user=alice', expectedHeaders);
    });

    it('includes newEngine filter when true', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with newEngine filter set to true
      await fetchSampledQueries(1, 20, undefined, undefined, true);

      // Assert: Verify URL includes newEngine=true parameter and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20&newEngine=true', expectedHeaders);
    });

    it('includes newEngine filter when false', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with newEngine filter set to false
      await fetchSampledQueries(1, 20, undefined, undefined, false);

      // Assert: Verify URL includes newEngine=false parameter and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20&newEngine=false', expectedHeaders);
    });

    it('combines multiple filters correctly', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with all filters
      await fetchSampledQueries(2, 50, 'tenant-b', 'bob', true);

      // Assert: Verify URL includes all parameters and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=2&pageSize=50&tenant=tenant-b&user=bob&newEngine=true', expectedHeaders);
    });

    it('omits tenant parameter when value is "all"', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with tenant set to "all"
      await fetchSampledQueries(1, 20, 'all');

      // Assert: Verify URL doesn't include tenant parameter but includes tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20', expectedHeaders);
    });

    it('omits user parameter when value is "all"', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Call with user set to "all"
      await fetchSampledQueries(1, 20, undefined, 'all');

      // Assert: Verify URL doesn't include user parameter but includes tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20', expectedHeaders);
    });

    it('includes from and to time range parameters', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce(mockResponse());

      const from = new Date('2023-01-01T10:00:00Z');
      const to = new Date('2023-01-01T11:00:00Z');

      // Act: Call with time range filters
      await fetchSampledQueries(1, 20, undefined, undefined, undefined, from, to);

      // Assert: Verify URL includes time parameters and tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20&from=2023-01-01T10%3A00%3A00.000Z&to=2023-01-01T11%3A00%3A00.000Z', expectedHeaders);
    });
  });

  describe('real-world nginx scenarios', () => {
    it('works correctly in namespaced nginx environment', async () => {
      // Setup: Mock nginx environment
      mockLocation('/namespace/ops/ui/goldfish');
      mockAbsolutePath.mockReturnValue('/namespace/ops/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful response
      mockFetch.mockResolvedValueOnce(mockResponse());

      // Act: Make API call
      await fetchSampledQueries();

      // Assert: Verify correct nginx-prefixed URL is used with tracing headers
      expect(mockFetch).toHaveBeenCalledWith('/namespace/ops/ui/api/v1/goldfish/queries?page=1&pageSize=20', expectedHeaders);
    });
  });
});