import { fetchSampledQueries } from './goldfish-api';
import { OUTCOME_ALL, OUTCOME_MATCH } from '@/types/goldfish';
import { absolutePath } from '../util';

const mockAbsolutePath = absolutePath as jest.MockedFunction<typeof absolutePath>;

// Mock the util module to spy on absolutePath calls
jest.mock('../util', () => ({
  absolutePath: jest.fn(),
}));

// Mock fetch globally
const mockFetch = jest.fn();
global.fetch = mockFetch;

// Mock window.location for testing
const mockLocation = (pathname: string) => {
  delete (window as any).location;
  (window as any).location = { pathname };
};

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
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          queries: [],
          total: 0,
          page: 1,
          pageSize: 20,
        }),
      });

      // Act: Call the API function
      await fetchSampledQueries(1, 20, OUTCOME_ALL);

      // Assert: Verify absolutePath was called with correct relative path
      expect(mockAbsolutePath).toHaveBeenCalledWith('/api/v1/goldfish/queries');
      
      // Assert: Verify fetch was called with the constructed URL
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20');
    });

    it('preserves query parameters when using absolutePath', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/base/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          queries: [],
          total: 5,
          page: 2,
          pageSize: 15,
        }),
      });

      // Act: Call with specific parameters including outcome filter
      await fetchSampledQueries(2, 15, OUTCOME_MATCH);

      // Assert: Verify the complete URL with all query parameters
      expect(mockFetch).toHaveBeenCalledWith('/base/api/v1/goldfish/queries?page=2&pageSize=15&outcome=match');
    });

    it('omits outcome parameter when outcome is OUTCOME_ALL', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful API response
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ queries: [], total: 0, page: 1, pageSize: 20 }),
      });

      // Act: Call with OUTCOME_ALL
      await fetchSampledQueries(1, 20, OUTCOME_ALL);

      // Assert: Verify URL doesn't include outcome parameter
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/goldfish/queries?page=1&pageSize=20');
    });

    it('handles API errors correctly', async () => {
      // Setup: Mock absolutePath
      mockAbsolutePath.mockReturnValue('/ui/api/v1/goldfish/queries');
      
      // Setup: Mock API error response
      mockFetch.mockResolvedValueOnce({
        ok: false,
        statusText: 'Internal Server Error',
      });

      // Act & Assert: Verify error is thrown
      await expect(fetchSampledQueries(1, 20)).rejects.toThrow('Failed to fetch sampled queries: Internal Server Error');
      
      // Assert: Verify absolutePath was still called
      expect(mockAbsolutePath).toHaveBeenCalledWith('/api/v1/goldfish/queries');
    });
  });

  describe('real-world nginx scenarios', () => {
    it('works correctly in namespaced nginx environment', async () => {
      // Setup: Mock nginx environment
      mockLocation('/namespace/ops/ui/goldfish');
      mockAbsolutePath.mockReturnValue('/namespace/ops/ui/api/v1/goldfish/queries');
      
      // Setup: Mock successful response
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: async () => ({ queries: [], total: 0, page: 1, pageSize: 20 }),
      });

      // Act: Make API call
      await fetchSampledQueries();

      // Assert: Verify correct nginx-prefixed URL is used
      expect(mockFetch).toHaveBeenCalledWith('/namespace/ops/ui/api/v1/goldfish/queries?page=1&pageSize=20');
    });
  });
});
