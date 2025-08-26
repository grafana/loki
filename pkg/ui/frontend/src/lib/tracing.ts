/**
 * Tracing utilities for frontend-initiated distributed tracing
 * Uses W3C Trace Context standard for trace propagation
 */

/**
 * Generates a random 64-bit hex trace ID
 */
export function generateTraceId(): string {
  // Generate 16 random bytes (128 bits) for trace ID
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
}

/**
 * Generates a random 32-bit hex span ID
 */
export function generateSpanId(): string {
  // Generate 8 random bytes (64 bits) for span ID
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
}

/**
 * Creates W3C Trace Context headers for a request
 * @param traceId - The trace ID to use
 * @param parentSpanId - The parent span ID (optional)
 * @param spanId - The current span ID
 */
export function createTraceHeaders(
  traceId: string,
  spanId: string,
  parentSpanId?: string
): Record<string, string> {
  // W3C Trace Context format
  // traceparent: version-traceid-spanid-flags
  const traceparent = `00-${traceId}-${spanId}-01`; // 01 flag means sampled
  
  const headers: Record<string, string> = {
    'traceparent': traceparent,
    'X-Trace-Id': traceId,
    'X-Span-Id': spanId,
  };
  
  if (parentSpanId) {
    headers['X-Parent-Span-Id'] = parentSpanId;
  }
  
  return headers;
}

/**
 * Extracts trace ID from error response or headers
 */
export function extractTraceId(
  response: Response | null,
  error: any
): string | null {
  // Try to get from response headers first
  if (response) {
    const traceId = response.headers.get('X-Trace-Id');
    if (traceId) return traceId;
  }
  
  // Try to get from error object if it contains trace info
  if (error?.traceId) {
    return error.traceId;
  }
  
  // Try to parse from error response body
  if (error?.response?.data?.traceId) {
    return error.response.data.traceId;
  }
  
  return null;
}

/**
 * Formats a trace ID for display
 */
export function formatTraceId(traceId: string): string {
  // Display first 8 and last 4 characters for brevity
  if (traceId.length > 16) {
    return `${traceId.slice(0, 8)}...${traceId.slice(-4)}`;
  }
  return traceId;
}

/**
 * Creates a Grafana Explore URL for viewing a trace
 */
export function createTraceExploreUrl(
  traceId: string,
  datasourceUid?: string,
  baseUrl?: string
): string | null {
  if (!datasourceUid || !baseUrl) {
    // If no datasource is configured, return null
    return null;
  }
  
  const exploreState = {
    datasource: datasourceUid,
    queries: [{
      refId: 'A',
      query: traceId,
      datasource: {
        type: 'tempo',
        uid: datasourceUid,
      },
      queryType: 'traceql',
      limit: 20,
      tableType: 'traces',
    }],
    range: {
      from: 'now-1h',
      to: 'now',
    },
  };
  
  const stateJson = JSON.stringify({
    'goldfish-trace-explore': exploreState,
  });
  
  const encodedState = encodeURIComponent(stateJson);
  return `${baseUrl}/explore?schemaVersion=1&panes=${encodedState}`;
}

/**
 * Trace context for a request
 */
export interface TraceContext {
  traceId: string;
  spanId: string;
  parentSpanId?: string;
  startTime: number;
}

/**
 * Creates a new trace context for a request
 */
export function createTraceContext(parentSpanId?: string): TraceContext {
  return {
    traceId: generateTraceId(),
    spanId: generateSpanId(),
    parentSpanId,
    startTime: Date.now(),
  };
}