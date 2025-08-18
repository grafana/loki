import { getBasename, absolutePath } from './util';

// Mock window.location for testing
const mockLocation = (pathname: string) => {
  delete (window as any).location;
  (window as any).location = { pathname };
};

describe('util functions', () => {
  afterEach(() => {
    // Reset to a default location after each test
    mockLocation('/ui/');
  });

  describe('getBasename', () => {
    it('extracts basename from standard /ui/ path', () => {
      mockLocation('/ui/');
      expect(getBasename()).toBe('/ui/');
    });

    it('extracts basename from nginx subpath with /ui/', () => {
      mockLocation('/loki-dev-005/ops/ui/some/path');
      expect(getBasename()).toBe('/loki-dev-005/ops/ui/');
    });

    it('returns default /ui/ when no match is found', () => {
      mockLocation('/some/random/path');
      expect(getBasename()).toBe('/ui/');
    });

    it('handles path ending with /ui/', () => {
      mockLocation('/loki-dev-ssd/ui/');
      expect(getBasename()).toBe('/loki-dev-ssd/ui/');
    });

    it('handles complex nginx paths', () => {
      mockLocation('/loki-live/ops/ui/features/goldfish');
      expect(getBasename()).toBe('/loki-live/ops/ui/');
    });
  });

  describe('absolutePath', () => {
    it('constructs correct path for standard /ui/ environment', () => {
      mockLocation('/ui/');
      expect(absolutePath('/api/v1/features')).toBe('/ui/api/v1/features');
    });

    it('constructs correct path for nginx subpath environment', () => {
      mockLocation('/loki-dev-005/ops/ui/dashboard');
      expect(absolutePath('/api/v1/features')).toBe('/loki-dev-005/ops/ui/api/v1/features');
    });

    it('handles path without leading slash', () => {
      mockLocation('/loki-dev-006/ui/');
      expect(absolutePath('api/v1/goldfish/queries')).toBe('/loki-dev-006/ui/api/v1/goldfish/queries');
    });

    it('handles empty path', () => {
      mockLocation('/ui/');
      expect(absolutePath('')).toBe('/ui/');
    });

    it('handles root path', () => {
      mockLocation('/ui/');
      expect(absolutePath('/')).toBe('/ui/');
    });

    it('constructs goldfish API path correctly in namespaced nginx environment', () => {
      mockLocation('/namespace/ops/ui/goldfish');
      expect(absolutePath('/api/v1/goldfish/queries')).toBe('/namespace/ops/ui/api/v1/goldfish/queries');
    });

    it('works with complex nginx paths', () => {
      mockLocation('/namespace/ops/ui/some/deep/path');
      expect(absolutePath('/api/v1/cluster/nodes')).toBe('/namespace/ops/ui/api/v1/cluster/nodes');
    });

    it('handles multiple consecutive slashes', () => {
      mockLocation('/namespace/ui/');
      // Current implementation preserves the extra slash, which is acceptable
      expect(absolutePath('//api/v1/features')).toBe('/namespace/ui//api/v1/features');
    });
  });

  describe('real-world nginx scenarios', () => {
    it('handles namespaced nginx configuration', () => {
      mockLocation('/namespace/ops/ui/rings/ingester');
      
      // Test features API
      expect(absolutePath('/api/v1/features')).toBe('/namespace/ops/ui/api/v1/features');
      
      // Test goldfish API
      expect(absolutePath('/api/v1/goldfish/queries')).toBe('/namespace/ops/ui/api/v1/goldfish/queries');
      
      // Test cluster API
      expect(absolutePath('/api/v1/cluster/nodes')).toBe('/namespace/ops/ui/api/v1/cluster/nodes');
    });

    it('handles local development environment', () => {
      mockLocation('/ui/');
      
      expect(absolutePath('/api/v1/features')).toBe('/ui/api/v1/features');
      expect(absolutePath('/api/v1/goldfish/queries')).toBe('/ui/api/v1/goldfish/queries');
    });
  });
});
