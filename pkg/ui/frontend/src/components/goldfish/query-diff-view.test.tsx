import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryDiffView } from './query-diff-view';
import { SampledQuery } from '@/types/goldfish';
import '@testing-library/jest-dom';

// Mock the feature flags context
jest.mock('@/contexts/use-feature-flags', () => ({
  useFeatureFlags: jest.fn(),
}));

import { useFeatureFlags } from '@/contexts/use-feature-flags';
const mockUseFeatureFlags = useFeatureFlags as jest.MockedFunction<typeof useFeatureFlags>;

describe('QueryDiffView - Trace ID Display', () => {
  beforeEach(() => {
    // Mock the feature flags for all tests in this describe block
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
        },
      },
      isLoading: false,
      error: null,
    });
  });

  const baseQuery: SampledQuery = {
    correlationId: 'test-correlation-1',
    tenantId: 'test-tenant',
    query: 'sum(rate(http_requests_total[5m]))',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 120,
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
    cellBEntriesReturned: 10,
    cellASplits: 1,
    cellBSplits: 1,
    cellAShards: 2,
    cellBShards: 2,
    cellAResponseHash: 'hash-a',
    cellBResponseHash: 'hash-a',
    cellAResponseSize: 500,
    cellBResponseSize: 550,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: null,
    cellBTraceID: null,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
    comparisonStatus: 'match',
  };

  it('displays trace IDs when they exist', async () => {
    const user = userEvent.setup();
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
    };

    const { container } = render(<QueryDiffView query={queryWithTraceIDs} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Test that trace IDs are displayed
    expect(screen.getByText('trace-123-abc')).toBeInTheDocument();
    expect(screen.getByText('trace-456-def')).toBeInTheDocument();
  });

  it('displays "N/A" when trace IDs are null', async () => {
    const user = userEvent.setup();
    const { container } = render(<QueryDiffView query={baseQuery} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Should show N/A for both trace IDs
    const naElements = screen.getAllByText('N/A');
    // We expect at least 2 N/A elements for the trace IDs
    // (there might be more N/A elements for other null fields)
    expect(naElements.length).toBeGreaterThanOrEqual(2);
  });

  it('displays trace ID for Cell A when only Cell A has trace ID', async () => {
    const user = userEvent.setup();
    const queryWithCellATrace: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-only-a',
      cellBTraceID: null,
    };

    const { container } = render(<QueryDiffView query={queryWithCellATrace} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    expect(screen.getByText('trace-only-a')).toBeInTheDocument();
    // Should still have at least one N/A for Cell B
    expect(screen.getByText('N/A')).toBeInTheDocument();
  });

  it('displays trace ID for Cell B when only Cell B has trace ID', async () => {
    const user = userEvent.setup();
    const queryWithCellBTrace: SampledQuery = {
      ...baseQuery,
      cellATraceID: null,
      cellBTraceID: 'trace-only-b',
    };

    const { container } = render(<QueryDiffView query={queryWithCellBTrace} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    expect(screen.getByText('trace-only-b')).toBeInTheDocument();
    // Should still have at least one N/A for Cell A
    expect(screen.getByText('N/A')).toBeInTheDocument();
  });
});

describe('QueryDiffView - Trace ID Links', () => {
  beforeEach(() => {
    // Mock the feature flags for all tests in this describe block
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
        },
      },
      isLoading: false,
      error: null,
    });
  });

  const baseQuery: SampledQuery = {
    correlationId: 'test-correlation-1',
    tenantId: 'test-tenant',
    query: 'sum(rate(http_requests_total[5m]))',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 120,
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
    cellBEntriesReturned: 10,
    cellASplits: 1,
    cellBSplits: 1,
    cellAShards: 2,
    cellBShards: 2,
    cellAResponseHash: 'hash-a',
    cellBResponseHash: 'hash-a',
    cellAResponseSize: 500,
    cellBResponseSize: 550,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: null,
    cellBTraceID: null,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
    comparisonStatus: 'match',
  };

  it('renders trace IDs as clickable links when they exist', async () => {
    const user = userEvent.setup();
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
      cellATraceLink: 'https://grafana.example.com/explore?trace-123-abc',
      cellBTraceLink: 'https://grafana.example.com/explore?trace-456-def',
    };

    const { container } = render(<QueryDiffView query={queryWithTraceIDs} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Find the links by their text content
    const cellALink = screen.getByText('trace-123-abc');
    const cellBLink = screen.getByText('trace-456-def');

    // Test that they are wrapped in anchor tags
    expect(cellALink.closest('a')).toBeInTheDocument();
    expect(cellBLink.closest('a')).toBeInTheDocument();
    
    // Test that they have href attributes (even if just "#" for now)
    expect(cellALink.closest('a')).toHaveAttribute('href');
    expect(cellBLink.closest('a')).toHaveAttribute('href');
  });

  it('does not render N/A text as a link when trace ID is null', async () => {
    const user = userEvent.setup();
    const { container } = render(<QueryDiffView query={baseQuery} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Find all N/A texts
    const naElements = screen.getAllByText('N/A');
    
    // Verify none of them are wrapped in anchor tags
    naElements.forEach(element => {
      expect(element.closest('a')).not.toBeInTheDocument();
    });
  });

  it('renders only existing trace ID as link when one is null', async () => {
    const user = userEvent.setup();
    const queryWithOneTraceID: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-exists',
      cellBTraceID: null,
      cellATraceLink: 'https://grafana.example.com/explore?trace-exists',
      cellBTraceLink: null,
    };

    const { container } = render(<QueryDiffView query={queryWithOneTraceID} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // The existing trace ID should be a link
    const traceLink = screen.getByText('trace-exists');
    expect(traceLink.closest('a')).toBeInTheDocument();

    // The N/A should not be a link
    const naElement = screen.getByText('N/A');
    expect(naElement.closest('a')).not.toBeInTheDocument();
  });
});

describe('QueryDiffView - Trace ID Visual Indicators', () => {
  beforeEach(() => {
    // Mock the feature flags for all tests in this describe block
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
        },
      },
      isLoading: false,
      error: null,
    });
  });

  const baseQuery: SampledQuery = {
    correlationId: 'test-correlation-1',
    tenantId: 'test-tenant',
    query: 'sum(rate(http_requests_total[5m]))',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 120,
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
    cellBEntriesReturned: 10,
    cellASplits: 1,
    cellBSplits: 1,
    cellAShards: 2,
    cellBShards: 2,
    cellAResponseHash: 'hash-a',
    cellBResponseHash: 'hash-a',
    cellAResponseSize: 500,
    cellBResponseSize: 550,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: null,
    cellBTraceID: null,
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
    comparisonStatus: 'match',
  };

  it('displays activity icon next to Trace ID label', async () => {
    const user = userEvent.setup();
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
    };

    const { container } = render(<QueryDiffView query={queryWithTraceIDs} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Find the section by its header
    const traceSection = screen.getByText('Trace IDs').closest('div');
    
    // Look for an icon (SVG element) within the trace section
    const icon = traceSection?.querySelector('svg');
    expect(icon).toBeInTheDocument();
  });

  it('applies link styling to trace ID links', async () => {
    const user = userEvent.setup();
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
      cellATraceLink: 'https://grafana.example.com/explore?trace-123-abc',
      cellBTraceLink: 'https://grafana.example.com/explore?trace-456-def',
    };

    const { container } = render(<QueryDiffView query={queryWithTraceIDs} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    const traceLink = screen.getByText('trace-123-abc').closest('a');
    
    // Check that link has appropriate styling classes
    expect(traceLink).toHaveClass('text-blue-600');
    expect(traceLink).toHaveClass('hover:text-blue-800');
    expect(traceLink).toHaveClass('hover:underline');
  });
});

describe('QueryDiffView - Namespace Display in Cell Labels', () => {
  const baseQuery: SampledQuery = {
    correlationId: 'test-correlation-1',
    tenantId: 'test-tenant',
    query: 'sum(rate(http_requests_total[5m]))',
    queryType: 'instant',
    startTime: '2024-01-01T00:00:00Z',
    endTime: '2024-01-01T01:00:00Z',
    stepDuration: null,
    cellAExecTimeMs: 100,
    cellBExecTimeMs: 120,
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
    cellBEntriesReturned: 10,
    cellASplits: 1,
    cellBSplits: 1,
    cellAShards: 2,
    cellBShards: 2,
    cellAResponseHash: 'hash-a',
    cellBResponseHash: 'hash-a',
    cellAResponseSize: 500,
    cellBResponseSize: 550,
    cellAStatusCode: 200,
    cellBStatusCode: 200,
    cellATraceID: 'trace-123',
    cellBTraceID: 'trace-456',
    sampledAt: '2024-01-01T00:00:00Z',
    createdAt: '2024-01-01T00:00:00Z',
    comparisonStatus: 'match',
  };

  beforeEach(() => {
    // Default mock - no namespaces
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
        },
      },
      isLoading: false,
      error: null,
    });
  });

  it('displays "Cell A" and "Cell B" when namespaces are not available', async () => {
    const user = userEvent.setup();
    const { container } = render(<QueryDiffView query={baseQuery} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Find the performance metrics header
    expect(screen.getByText('Cell A')).toBeInTheDocument();
    expect(screen.getByText('Cell B')).toBeInTheDocument();
    
    // Should not contain namespace in parentheses
    expect(screen.queryByText(/Cell A \(/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Cell B \(/)).not.toBeInTheDocument();
  });

  it('displays "Cell A (loki-ops-002)" when Cell A namespace is available', async () => {
    const user = userEvent.setup();
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
          cellANamespace: 'loki-ops-002',
          cellBNamespace: 'loki-ops-003',
        },
      },
      isLoading: false,
      error: null,
    });

    const { container } = render(<QueryDiffView query={baseQuery} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Should display namespaces in parentheses
    expect(screen.getByText('Cell A (loki-ops-002)')).toBeInTheDocument();
    expect(screen.getByText('Cell B (loki-ops-003)')).toBeInTheDocument();
  });

  it('displays namespace for only Cell A when only Cell A namespace is available', async () => {
    const user = userEvent.setup();
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
          cellANamespace: 'loki-ops-002',
          // cellBNamespace is undefined
        },
      },
      isLoading: false,
      error: null,
    });

    const { container } = render(<QueryDiffView query={baseQuery} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Cell A should have namespace
    expect(screen.getByText('Cell A (loki-ops-002)')).toBeInTheDocument();
    // Cell B should not have namespace
    expect(screen.getByText('Cell B')).toBeInTheDocument();
    expect(screen.queryByText(/Cell B \(/)).not.toBeInTheDocument();
  });

  it('displays namespace for only Cell B when only Cell B namespace is available', async () => {
    const user = userEvent.setup();
    mockUseFeatureFlags.mockReturnValue({
      features: {
        goldfish: {
          enabled: true,
          cellBNamespace: 'loki-ops-003',
          // cellANamespace is undefined
        },
      },
      isLoading: false,
      error: null,
    });

    const { container } = render(<QueryDiffView query={baseQuery} />);

    // Expand the collapsible content to see the trace IDs
    const trigger = container.querySelector('[type="button"]');
    if (trigger) await user.click(trigger);

    // Cell A should not have namespace
    expect(screen.getByText('Cell A')).toBeInTheDocument();
    expect(screen.queryByText(/Cell A \(/)).not.toBeInTheDocument();
    // Cell B should have namespace
    expect(screen.getByText('Cell B (loki-ops-003)')).toBeInTheDocument();
  });
});