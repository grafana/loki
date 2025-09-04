import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OutcomeFilter } from '@/types/goldfish';

const QUERY_OPTIONS = {
  staleTime: 5 * 60 * 1000, // 5 minutes
  gcTime: 10 * 60 * 1000, // 10 minutes
};

export function useGoldfishQueries(
  page: number, 
  pageSize: number, 
  selectedOutcome: OutcomeFilter,
  tenant?: string,
  user?: string,
  newEngine?: boolean,
  from?: Date | null,
  to?: Date | null
) {
  const [currentTraceId, setCurrentTraceId] = useState<string | null>(null);
  
  // Query fetches all data, filtering happens in frontend
  const query = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page, pageSize, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      setCurrentTraceId(result.traceId);
      
      if (result.error) {
        throw result.error;
      }
      
      return result.data;
    },
    ...QUERY_OPTIONS,
  });

  // Filter queries based on selectedOutcome in the frontend
  const filteredQueries = useMemo(() => {
    if (!query.data?.queries) return [];
    
    if (selectedOutcome && selectedOutcome !== 'all') {
      return query.data.queries.filter(q => q.comparisonStatus === selectedOutcome);
    }
    
    return query.data.queries;
  }, [query.data?.queries, selectedOutcome]);

  const refresh = () => {
    query.refetch();
  };

  return {
    queries: filteredQueries,
    isLoading: query.isLoading,
    error: query.error,
    hasMore: query.data?.hasMore || false,
    page: query.data?.page || page,
    pageSize: query.data?.pageSize || pageSize,
    refresh,
    traceId: currentTraceId,
  };
}