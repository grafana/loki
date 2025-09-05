import { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OutcomeFilter, SampledQuery } from '@/types/goldfish';

const QUERY_OPTIONS = {
  staleTime: 1000, // 1 second - very short to ensure fresh data on filter changes
  gcTime: 10 * 60 * 1000, // 10 minutes
  refetchOnMount: 'always' as const, // Always refetch when component mounts or query key changes
};

export function useGoldfishQueries(
  pageSize: number, 
  selectedOutcome: OutcomeFilter,
  tenant?: string,
  user?: string,
  newEngine?: boolean,
  from?: Date | null,
  to?: Date | null
) {
  const queryClient = useQueryClient();
  const [currentTraceId, setCurrentTraceId] = useState<string | null>(null);
  const [allQueries, setAllQueries] = useState<SampledQuery[]>([]);
  const [hasMore, setHasMore] = useState(true);
  
  // Track current page
  const [currentPage, setCurrentPage] = useState(1);
  
  // Create a stable filter key for tracking changes
  const filterKey = `${tenant ?? ''}-${user ?? ''}-${newEngine ?? ''}-${from?.getTime() ?? ''}-${to?.getTime() ?? ''}`;
  const prevFilterKeyRef = useRef<string | undefined>(undefined);
  
  useEffect(() => {
    if (prevFilterKeyRef.current !== undefined && prevFilterKeyRef.current !== filterKey) {
      setCurrentPage(1);
    }
    prevFilterKeyRef.current = filterKey;
  }, [filterKey]);
  
  // Single query - always sends ALL filters to backend
  const query = useQuery({
    queryKey: ['goldfish-queries', currentPage, pageSize, tenant, user, newEngine, from, to],
    queryFn: async () => {
      const result = await fetchSampledQueries(
        currentPage, 
        pageSize, 
        tenant, 
        user, 
        newEngine, 
        from ?? undefined, 
        to ?? undefined
      );
      
      setCurrentTraceId(result.traceId);
      
      if (result.error) {
        throw result.error;
      }
      
      return result.data;
    },
    ...QUERY_OPTIONS,
  });

  // Update state when data arrives - simple rule: page 1 replaces, page 2+ appends
  useEffect(() => {
    if (query.data) {
      const newQueries = query.data.queries || [];
      
      if (currentPage === 1) {
        // Page 1 means fresh start (either filter change or refresh)
        setAllQueries(newQueries);
      } else {
        // Page 2+ means load more
        setAllQueries(prev => [...prev, ...newQueries]);
      }
      
      setHasMore(query.data?.hasMore || false);
    }
  }, [query.data, currentPage]);

  // Apply outcome filter on frontend for immediate response and sort
  const displayQueries = useMemo(() => {
    return allQueries
      .filter(q => {
        if (selectedOutcome && selectedOutcome !== 'all') {
          return q.comparisonStatus === selectedOutcome;
        }
        return true;
      })
      .sort((a, b) => 
        new Date(b.sampledAt).getTime() - new Date(a.sampledAt).getTime()
      );
  }, [allQueries, selectedOutcome]);

  // Load more function - increments page counter
  const loadMore = useCallback(() => {
    if (!query.isLoading && hasMore) {
      setCurrentPage(prev => prev + 1);
    }
  }, [query.isLoading, hasMore]);

  // Refresh function - invalidate cache and reset to page 1
  const refresh = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: ['goldfish-queries'] });
    setCurrentPage(1);
  }, [queryClient]);

  return {
    queries: displayQueries,
    isLoading: query.isLoading && allQueries.length === 0, // Only show loading on initial load
    isLoadingMore: query.isLoading && currentPage > 1,
    error: query.error,
    hasMore,
    loadMore,
    refresh,
    traceId: currentTraceId,
  };
}
