import { 
  generateTraceId,
  generateSpanId,
  createTraceHeaders,
  extractTraceId,
  formatTraceId,
  createTraceExploreUrl,
  createTraceContext,
} from './tracing';

describe('tracing utilities', () => {
  describe('generateTraceId', () => {
    it('generates valid 32-character hex trace IDs', () => {
      const traceId = generateTraceId();
      expect(traceId).toMatch(/^[0-9a-f]{32}$/);
      expect(traceId.length).toBe(32);
    });

    it('generates unique trace IDs', () => {
      const ids = new Set();
      for (let i = 0; i < 100; i++) {
        ids.add(generateTraceId());
      }
      expect(ids.size).toBe(100);
    });
  });

  describe('generateSpanId', () => {
    it('generates valid 16-character hex span IDs', () => {
      const spanId = generateSpanId();
      expect(spanId).toMatch(/^[0-9a-f]{16}$/);
      expect(spanId.length).toBe(16);
    });

    it('generates unique span IDs', () => {
      const ids = new Set();
      for (let i = 0; i < 100; i++) {
        ids.add(generateSpanId());
      }
      expect(ids.size).toBe(100);
    });
  });

  describe('createTraceHeaders', () => {
    it('creates correct W3C trace context headers', () => {
      const traceId = '0123456789abcdef0123456789abcdef';
      const spanId = '0123456789abcdef';
      
      const headers = createTraceHeaders(traceId, spanId);
      
      expect(headers.traceparent).toBe(`00-${traceId}-${spanId}-01`);
      expect(headers['X-Trace-Id']).toBe(traceId);
      expect(headers['X-Span-Id']).toBe(spanId);
    });

    it('handles lowercase trace and span IDs', () => {
      const traceId = 'abcdef0123456789abcdef0123456789';
      const spanId = 'fedcba9876543210';
      
      const headers = createTraceHeaders(traceId, spanId);
      
      expect(headers.traceparent).toBe(`00-${traceId}-${spanId}-01`);
    });

    it('creates valid traceparent format', () => {
      const traceId = generateTraceId();
      const spanId = generateSpanId();
      
      const headers = createTraceHeaders(traceId, spanId);
      
      // W3C Trace Context format: version-traceId-spanId-flags
      expect(headers.traceparent).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/);
    });
  });

  describe('extractTraceId', () => {
    it('extracts trace ID from response headers', () => {
      const response = {
        headers: {
          get: (key: string) => key === 'X-Trace-Id' ? 'test-trace-123' : null
        }
      } as unknown as Response;
      
      const traceId = extractTraceId(response, null);
      expect(traceId).toBe('test-trace-123');
    });

    it('extracts trace ID from error with traceId', () => {
      const error = { traceId: 'error-trace-456' };
      
      const traceId = extractTraceId(null, error);
      expect(traceId).toBe('error-trace-456');
    });

    it('extracts trace ID from error response data', () => {
      const error = {
        response: {
          data: {
            traceId: 'response-trace-789'
          }
        }
      };
      
      const traceId = extractTraceId(null, error);
      expect(traceId).toBe('response-trace-789');
    });

    it('returns null when no traceId is present', () => {
      const response = {
        headers: {
          get: () => null
        }
      } as unknown as Response;
      
      const traceId = extractTraceId(response, null);
      expect(traceId).toBeNull();
    });

    it('returns null for invalid inputs', () => {
      expect(extractTraceId(null, null)).toBeNull();
      expect(extractTraceId(null, undefined)).toBeNull();
      expect(extractTraceId(null, {})).toBeNull();
    });
  });


  describe('createTraceContext', () => {
    it('creates a new trace context with generated IDs', () => {
      const context = createTraceContext();
      
      expect(context.traceId).toMatch(/^[0-9a-f]{32}$/);
      expect(context.spanId).toMatch(/^[0-9a-f]{16}$/);
      expect(context.parentSpanId).toBeUndefined();
      expect(context.startTime).toBeGreaterThan(0);
    });

    it('creates a trace context with parent span ID', () => {
      const parentSpanId = 'parent123';
      const context = createTraceContext(parentSpanId);
      
      expect(context.parentSpanId).toBe(parentSpanId);
    });
  });

  describe('formatTraceId', () => {
    it('formats long trace IDs with ellipsis', () => {
      const longTraceId = '0123456789abcdef0123456789abcdef';
      const formatted = formatTraceId(longTraceId);
      
      expect(formatted).toBe('01234567...cdef');
    });

    it('returns short trace IDs unchanged', () => {
      const shortTraceId = '0123456789abcdef';
      const formatted = formatTraceId(shortTraceId);
      
      expect(formatted).toBe(shortTraceId);
    });
  });

  describe('createTraceExploreUrl', () => {
    it('creates Grafana explore URL with trace ID', () => {
      const traceId = 'test-trace-123';
      const url = createTraceExploreUrl(traceId, 'tempo-uid', 'http://localhost:3000');
      
      expect(url).toContain('/explore');
      expect(url).toContain('schemaVersion=1');
      expect(url).toContain('panes=');
      expect(url).toContain(encodeURIComponent(traceId));
    });

    it('returns null when datasource is not configured', () => {
      const traceId = 'test-trace-123';
      const url = createTraceExploreUrl(traceId, undefined, 'http://localhost:3000');
      
      expect(url).toBeNull();
    });

    it('encodes trace ID properly', () => {
      const traceId = 'trace-with-special-chars!@#$%';
      const url = createTraceExploreUrl(traceId, 'tempo-uid', 'http://localhost:3000');
      
      expect(url).toContain(encodeURIComponent(traceId));
    });

    it('uses provided base URL', () => {
      const traceId = 'test-trace';
      const url = createTraceExploreUrl(traceId, 'tempo-uid', 'https://grafana.example.com:8080');
      
      expect(url).toMatch(/^https:\/\/grafana\.example\.com:8080/);
    });

    it('constructs correct explore URL structure', () => {
      const traceId = 'abc123';
      const datasourceUid = 'tempo-uid';
      const url = createTraceExploreUrl(traceId, datasourceUid, 'http://localhost:3000');
      
      expect(url).not.toBeNull();
      if (url) {
        const urlObj = new URL(url);
        expect(urlObj.pathname).toBe('/explore');
        
        const encodedState = urlObj.searchParams.get('panes');
        expect(encodedState).toBeTruthy();
        
        const state = JSON.parse(decodeURIComponent(encodedState!));
        const exploreState = state['goldfish-trace-explore'];
        expect(exploreState.datasource).toBe(datasourceUid);
        expect(exploreState.queries).toHaveLength(1);
        expect(exploreState.queries[0].query).toBe(traceId);
      }
    });

    it('includes time range in explore URL', () => {
      const traceId = 'test-trace';
      const url = createTraceExploreUrl(traceId, 'tempo-uid', 'http://localhost:3000');
      
      expect(url).not.toBeNull();
      if (url) {
        const urlObj = new URL(url);
        const encodedState = urlObj.searchParams.get('panes');
        const state = JSON.parse(decodeURIComponent(encodedState!));
        const exploreState = state['goldfish-trace-explore'];
        expect(exploreState.range).toEqual({
          from: 'now-1h',
          to: 'now'
        });
      }
    });
  });
});
