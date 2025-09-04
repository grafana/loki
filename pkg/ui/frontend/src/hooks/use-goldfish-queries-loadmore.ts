import { useState, useCallback, useEffect, useMemo } from 'react';
import { useQuery } from '@tanstack/react-query';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OutcomeFilter, SampledQuery } from '@/types/goldfish';

const QUERY_OPTIONS = {
  staleTime: 1000, // 1 second - very short to ensure fresh data on filter changes
  gcTime: 10 * 60 * 1000, // 10 minutes
  refetchOnMount: 'always' as const, // Always refetch when component mounts or query key changes
};

export function useGoldfishQueriesLoadMore(
  pageSize: number, 
  selectedOutcome: OutcomeFilter,
  tenant?: string,
  user?: string,
  newEngine?: boolean,
  from?: Date | null,
  to?: Date | null
) {
  const [currentTraceId, setCurrentTraceId] = useState<string | null>(null);
  const [allRawQueries, setAllRawQueries] = useState<SampledQuery[]>([]); // All fetched data
  const [currentPage, setCurrentPage] = useState(1);
  const [hasMoreGlobal, setHasMoreGlobal] = useState(false); // Track if there's more data on backend, regardless of filters
  
  // Main query - fetches initial page without filters for broad data
  const query = useQuery({
    queryKey: ['goldfish-queries-page', currentPage, pageSize, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      // Fetch without tenant/user filters for initial broad data
      const result = await fetchSampledQueries(currentPage, pageSize, undefined, undefined, newEngine, from ?? undefined, to ?? undefined);
      setCurrentTraceId(result.traceId);
      
      if (result.error) {
        throw result.error;
      }
      
      return result.data;
    },
    ...QUERY_OPTIONS,
  });
  
  // Supplemental query - fetches filtered data from backend when filters are applied
  const supplementalQuery = useQuery({
    queryKey: ['goldfish-queries-filtered', tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      // Only run if we have filters
      if (!tenant && !user) {
        return null;
      }
      
      // Fetch filtered data from backend
      const result = await fetchSampledQueries(1, pageSize * 2, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      
      if (result.error) {
        // Don't throw for supplemental query - just log it
        console.error('Supplemental query error:', result.error);
        return null;
      }
      
      return result.data;
    },
    enabled: !!(tenant || user), // Only run if filters are applied
    ...QUERY_OPTIONS,
  });

  // Update raw data when new data arrives from main query
  useEffect(() => {
    if (!query.isLoading && query.isSuccess && query.data) {
      const newQueries = query.data.queries || [];
      
      if (currentPage === 1) {
        // Reset for first page
        setAllRawQueries(newQueries);
      } else {
        // Append for subsequent pages - merge with existing data
        setAllRawQueries(prev => [...prev, ...newQueries]);
      }
      setHasMoreGlobal(query.data?.hasMore || false);
    }
  }, [query.data, query.isLoading, query.isSuccess, currentPage]);
  
  // Merge supplemental filtered data when it arrives
  useEffect(() => {
    if (supplementalQuery.isSuccess && supplementalQuery.data?.queries) {
      const supplementalQueries = supplementalQuery.data.queries;
      
      setAllRawQueries(prev => {
        // Create a map of existing queries by correlation ID to avoid duplicates
        const existingMap = new Map(prev.map(q => [q.correlationId, q]));
        
        // Add supplemental queries that aren't already in the map
        supplementalQueries.forEach(q => {
          if (!existingMap.has(q.correlationId)) {
            existingMap.set(q.correlationId, q);
          }
        });
        
        // Convert back to array and sort by sampled time
        return Array.from(existingMap.values()).sort((a, b) => 
          new Date(b.sampledAt).getTime() - new Date(a.sampledAt).getTime()
        );
      });
      
    }
  }, [supplementalQuery.data, supplementalQuery.isSuccess]);

  // Apply frontend filters to the raw data
  const filteredQueries = useMemo(() => {
    let result = [...allRawQueries];
    
    // Filter by tenant
    if (tenant) {
      result = result.filter(q => q.tenantId === tenant);
    }
    
    // Filter by user
    if (user) {
      result = result.filter(q => q.user === user);
    }
    
    // Filter by outcome
    if (selectedOutcome && selectedOutcome !== 'all') {
      result = result.filter(q => q.comparisonStatus === selectedOutcome);
    }
    
    
    return result;
  }, [allRawQueries, tenant, user, selectedOutcome]);

  // Reset page when time range or engine filter changes (these require new backend fetch)
  useEffect(() => {
    setCurrentPage(1);
    setAllRawQueries([]);
    setHasMoreGlobal(false);
  }, [newEngine, pageSize, from?.toISOString(), to?.toISOString()]);

  // Load more function
  const loadMore = useCallback(() => {
    if (!query.isLoading && hasMoreGlobal) {
      setCurrentPage(prev => prev + 1);
    }
  }, [hasMoreGlobal, query.isLoading]);

  // Refresh function - resets everything and refetches
  const refresh = useCallback(() => {
    setCurrentPage(1);
    setAllRawQueries([]);
    setHasMoreGlobal(false);
    query.refetch();
  }, [query]);

  return {
    queries: filteredQueries,
    isLoading: query.isLoading && allRawQueries.length === 0, // Only show loading on initial load
    isLoadingMore: query.isLoading && currentPage > 1,
    isLoadingSupplemental: supplementalQuery.isLoading, // New field to indicate background loading
    error: query.error,
    hasMore: hasMoreGlobal, // Always return the global hasMore state
    loadMore,
    refresh,
    traceId: currentTraceId,
    currentPage,
  };
}
