import React from 'react';
import { render, screen } from '@testing-library/react';
import { QueryDiffView } from './query-diff-view';
import { SampledQuery } from '@/types/goldfish';
import '@testing-library/jest-dom';

describe('QueryDiffView - Trace ID Display', () => {
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

  it('displays trace IDs when they exist', () => {
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
    };

    render(<QueryDiffView query={queryWithTraceIDs} />);

    // Test that trace IDs are displayed
    expect(screen.getByText('trace-123-abc')).toBeInTheDocument();
    expect(screen.getByText('trace-456-def')).toBeInTheDocument();
  });

  it('displays "N/A" when trace IDs are null', () => {
    render(<QueryDiffView query={baseQuery} />);

    // Should show N/A for both trace IDs
    const naElements = screen.getAllByText('N/A');
    // We expect at least 2 N/A elements for the trace IDs
    // (there might be more N/A elements for other null fields)
    expect(naElements.length).toBeGreaterThanOrEqual(2);
  });

  it('displays trace ID for Cell A when only Cell A has trace ID', () => {
    const queryWithCellATrace: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-only-a',
      cellBTraceID: null,
    };

    render(<QueryDiffView query={queryWithCellATrace} />);

    expect(screen.getByText('trace-only-a')).toBeInTheDocument();
    // Should still have at least one N/A for Cell B
    expect(screen.getByText('N/A')).toBeInTheDocument();
  });

  it('displays trace ID for Cell B when only Cell B has trace ID', () => {
    const queryWithCellBTrace: SampledQuery = {
      ...baseQuery,
      cellATraceID: null,
      cellBTraceID: 'trace-only-b',
    };

    render(<QueryDiffView query={queryWithCellBTrace} />);

    expect(screen.getByText('trace-only-b')).toBeInTheDocument();
    // Should still have at least one N/A for Cell A
    expect(screen.getByText('N/A')).toBeInTheDocument();
  });
});

describe('QueryDiffView - Trace ID Links', () => {
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

  it('renders trace IDs as clickable links when they exist', () => {
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
    };

    render(<QueryDiffView query={queryWithTraceIDs} />);

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

  it('does not render N/A text as a link when trace ID is null', () => {
    render(<QueryDiffView query={baseQuery} />);

    // Find all N/A texts
    const naElements = screen.getAllByText('N/A');
    
    // Verify none of them are wrapped in anchor tags
    naElements.forEach(element => {
      expect(element.closest('a')).not.toBeInTheDocument();
    });
  });

  it('renders only existing trace ID as link when one is null', () => {
    const queryWithOneTraceID: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-exists',
      cellBTraceID: null,
    };

    render(<QueryDiffView query={queryWithOneTraceID} />);

    // The existing trace ID should be a link
    const traceLink = screen.getByText('trace-exists');
    expect(traceLink.closest('a')).toBeInTheDocument();

    // The N/A should not be a link
    const naElement = screen.getByText('N/A');
    expect(naElement.closest('a')).not.toBeInTheDocument();
  });
});

describe('QueryDiffView - Trace ID Visual Indicators', () => {
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

  it('displays activity icon next to Trace ID label', () => {
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
    };

    render(<QueryDiffView query={queryWithTraceIDs} />);

    // Find the section by its header
    const traceSection = screen.getByText('Trace IDs').closest('div');
    
    // Look for an icon (SVG element) within the trace section
    const icon = traceSection?.querySelector('svg');
    expect(icon).toBeInTheDocument();
  });

  it('applies link styling to trace ID links', () => {
    const queryWithTraceIDs: SampledQuery = {
      ...baseQuery,
      cellATraceID: 'trace-123-abc',
      cellBTraceID: 'trace-456-def',
    };

    render(<QueryDiffView query={queryWithTraceIDs} />);

    const traceLink = screen.getByText('trace-123-abc').closest('a');
    
    // Check that link has appropriate styling classes
    expect(traceLink).toHaveClass('text-blue-600');
    expect(traceLink).toHaveClass('hover:text-blue-800');
    expect(traceLink).toHaveClass('hover:underline');
  });
});