import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '@testing-library/jest-dom';

// Mock fetch globally
const mockFetch = jest.fn();
global.fetch = mockFetch;

// Mock the environment abstraction to avoid import.meta issues
jest.mock('../lib/environment', () => ({
  shouldUseMockFeatures: jest.fn(),
  getDevEnvironmentOverrides: jest.fn(),
}));

// Mock the absolutePath utility
jest.mock('../util', () => ({
  absolutePath: jest.fn(),
}));

// Import after mocking
import { FeatureFlagsProvider } from './feature-flags';
import { useFeatureFlags } from './use-feature-flags';
import { shouldUseMockFeatures, getDevEnvironmentOverrides } from '../lib/environment';
import { absolutePath } from '../util';

const mockShouldUseMockFeatures = shouldUseMockFeatures as jest.MockedFunction<typeof shouldUseMockFeatures>;
const mockGetDevEnvironmentOverrides = getDevEnvironmentOverrides as jest.MockedFunction<typeof getDevEnvironmentOverrides>;
const mockAbsolutePath = absolutePath as jest.MockedFunction<typeof absolutePath>;

// Test component that uses the feature flags
function TestComponent() {
  const { features, isLoading, error } = useFeatureFlags();
  
  if (isLoading) return <div>Loading...</div>;
  if (error) return <div>Error</div>;
  
  return (
    <div>
      <div data-testid="goldfish-feature">{features.goldfish.enabled ? 'enabled' : 'disabled'}</div>
      <div data-testid="goldfish-cell-a-namespace">{features.goldfish.cellANamespace || 'none'}</div>
      <div data-testid="goldfish-cell-b-namespace">{features.goldfish.cellBNamespace || 'none'}</div>
    </div>
  );
}

function renderWithProvider() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        staleTime: 0,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <FeatureFlagsProvider>
        <TestComponent />
      </FeatureFlagsProvider>
    </QueryClientProvider>
  );
}

describe('FeatureFlagsProvider API calls with environment abstraction', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    // Default setup: not using mock features, no dev overrides
    mockShouldUseMockFeatures.mockReturnValue(false);
    mockGetDevEnvironmentOverrides.mockReturnValue({});
  });

  it('uses absolutePath to construct API URL for local development', async () => {
    // Setup: Mock absolutePath to return local development path
    mockAbsolutePath.mockReturnValue('/ui/api/v1/features');
    
    // Setup: Mock successful API response
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ goldfish: { enabled: true } }),
    });

    // Act: Render the provider which triggers the API call
    renderWithProvider();

    // Assert: Verify absolutePath was called with correct path
    await waitFor(() => {
      expect(mockAbsolutePath).toHaveBeenCalledWith('/api/v1/features');
    });

    // Assert: Verify fetch was called with the constructed URL
    expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/features');

    // Assert: Verify features are loaded
    await waitFor(() => {
      expect(screen.getByTestId('goldfish-feature')).toHaveTextContent('enabled');
    });
  });

  it('uses mock features when shouldUseMockFeatures returns true', async () => {
    // Setup: Enable mock features
    mockShouldUseMockFeatures.mockReturnValue(true);
    mockGetDevEnvironmentOverrides.mockReturnValue({ goldfish: { enabled: true } });

    // Act: Render the provider
    renderWithProvider();

    // Assert: Should not make any API calls when using mock features
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockAbsolutePath).not.toHaveBeenCalled();

    // Assert: Should use dev overrides
    await waitFor(() => {
      expect(screen.getByTestId('goldfish-feature')).toHaveTextContent('enabled');
    });
  });

  it('combines server response with dev overrides', async () => {
    // Setup: Mock server response and dev overrides
    mockAbsolutePath.mockReturnValue('/ui/api/v1/features');
    mockGetDevEnvironmentOverrides.mockReturnValue({ goldfish: { enabled: true } }); // Override to true
    
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({ goldfish: { enabled: false } }), // Server says false
    });

    // Act: Render the provider
    renderWithProvider();

    // Assert: Should make API call
    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledWith('/ui/api/v1/features');
    });

    // Assert: Dev override should take precedence
    await waitFor(() => {
      expect(screen.getByTestId('goldfish-feature')).toHaveTextContent('enabled');
    });
  });

  it('handles API errors gracefully', async () => {
    // Setup: Mock absolutePath and API error
    mockAbsolutePath.mockReturnValue('/ui/api/v1/features');
    mockFetch.mockRejectedValueOnce(new Error('Network error'));

    // Act: Render the provider
    renderWithProvider();

    // Assert: Should handle the error gracefully
    await waitFor(() => {
      expect(screen.getByText('Error')).toBeInTheDocument();
    }, { timeout: 3000 });

    // Assert: Should still have called absolutePath
    expect(mockAbsolutePath).toHaveBeenCalledWith('/api/v1/features');
  });

  it('handles goldfish object structure with namespaces', async () => {
    // Setup: Mock absolutePath and API response with goldfish object
    mockAbsolutePath.mockReturnValue('/ui/api/v1/features');
    
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        goldfish: {
          enabled: true,
          cellANamespace: 'loki-ops-002',
          cellBNamespace: 'loki-ops-003'
        }
      }),
    });

    // Act: Render the provider
    renderWithProvider();

    // Assert: Verify features are loaded with namespaces
    await waitFor(() => {
      expect(screen.getByTestId('goldfish-feature')).toHaveTextContent('enabled');
      expect(screen.getByTestId('goldfish-cell-a-namespace')).toHaveTextContent('loki-ops-002');
      expect(screen.getByTestId('goldfish-cell-b-namespace')).toHaveTextContent('loki-ops-003');
    });
  });

  it('handles goldfish object structure without namespaces', async () => {
    // Setup: Mock absolutePath and API response with goldfish object but no namespaces
    mockAbsolutePath.mockReturnValue('/ui/api/v1/features');
    
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        goldfish: {
          enabled: false
        }
      }),
    });

    // Act: Render the provider
    renderWithProvider();

    // Assert: Verify features are loaded without namespaces
    await waitFor(() => {
      expect(screen.getByTestId('goldfish-feature')).toHaveTextContent('disabled');
      expect(screen.getByTestId('goldfish-cell-a-namespace')).toHaveTextContent('none');
      expect(screen.getByTestId('goldfish-cell-b-namespace')).toHaveTextContent('none');
    });
  });
});
