import { useState, useCallback, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OutcomeFilter, SampledQuery } from '@/types/goldfish';

const QUERY_OPTIONS = {
  staleTime: 5 * 60 * 1000, // 5 minutes
  gcTime: 10 * 60 * 1000, // 10 minutes
};

export function useGoldfishQueriesLoadMore(
  pageSize: number, 
  selectedOutcome: OutcomeFilter,
  tenant?: string,
  user?: string,
  newEngine?: boolean,
  from?: Date,
  to?: Date
) {
  const [currentTraceId, setCurrentTraceId] = useState<string | null>(null);
  const [allQueries, setAllQueries] = useState<SampledQuery[]>([]);
  const [currentPage, setCurrentPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  
  // Query for current page
  const query = useQuery({
    queryKey: ['goldfish-queries-page', currentPage, pageSize, selectedOutcome, tenant, user, newEngine, from?.toISOString(), to?.toISOString()],
    queryFn: async () => {
      const result = await fetchSampledQueries(currentPage, pageSize, selectedOutcome, tenant, user, newEngine, from, to);
      setCurrentTraceId(result.traceId);
      
      if (result.error) {
        throw result.error;
      }
      
      return result.data;
    },
    ...QUERY_OPTIONS,
  });

  // Update allQueries when new data arrives
  useEffect(() => {
    if (query.data) {
      if (currentPage === 1) {
        // Reset for first page or when filters change
        setAllQueries(query.data.queries);
      } else {
        // Append for subsequent pages
        setAllQueries(prev => [...prev, ...query.data.queries]);
      }
      setHasMore(query.data?.hasMore || false);
    }
  }, [query.data, currentPage]);

  // Reset when filters change
  useEffect(() => {
    setCurrentPage(1);
    setAllQueries([]);
    setHasMore(false);
  }, [selectedOutcome, tenant, user, newEngine, pageSize, from?.toISOString(), to?.toISOString()]);

  // Load more function
  const loadMore = useCallback(() => {
    if (!query.isLoading && hasMore) {
      setCurrentPage(prev => prev + 1);
    }
  }, [hasMore, query.isLoading]);

  // Refresh function - resets everything and refetches
  const refresh = useCallback(() => {
    setCurrentPage(1);
    setAllQueries([]);
    setHasMore(false);
    query.refetch();
  }, [query]);

  return {
    queries: allQueries,
    isLoading: query.isLoading,
    isLoadingMore: query.isLoading && currentPage > 1,
    error: query.error,
    hasMore,
    loadMore,
    refresh,
    traceId: currentTraceId,
    currentPage,
  };
}