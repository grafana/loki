import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { fetchSampledQueries } from '@/lib/goldfish-api';
import { OutcomeFilter, OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR } from '@/types/goldfish';

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
  
  // Main query always fetches all data
  const mainQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_ALL, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page, pageSize, OUTCOME_ALL, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      setCurrentTraceId(result.traceId);
      
      if (result.error) {
        throw result.error;
      }
      
      return result.data;
    },
    ...QUERY_OPTIONS,
  });

  // Background queries for specific filters
  const matchQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_MATCH, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page, pageSize, OUTCOME_MATCH, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (selectedOutcome === OUTCOME_MATCH) {
        setCurrentTraceId(result.traceId);
      }
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: selectedOutcome === OUTCOME_MATCH,
    ...QUERY_OPTIONS,
  });

  const mismatchQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (selectedOutcome === OUTCOME_MISMATCH) {
        setCurrentTraceId(result.traceId);
      }
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: selectedOutcome === OUTCOME_MISMATCH,
    ...QUERY_OPTIONS,
  });

  const errorQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_ERROR, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page, pageSize, OUTCOME_ERROR, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (selectedOutcome === OUTCOME_ERROR) {
        setCurrentTraceId(result.traceId);
      }
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: selectedOutcome === OUTCOME_ERROR,
    ...QUERY_OPTIONS,
  });

  // Check if there are more pages
  const hasMore = useMemo(() => {
    return mainQuery.data ? mainQuery.data.hasMore : false;
  }, [mainQuery.data]);

  // Prefetch next page for main query
  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_ALL, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page + 1, pageSize, OUTCOME_ALL, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: hasMore && page >= 1,
    ...QUERY_OPTIONS,
  });

  // Prefetch next page for specific filter outcomes
  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_MATCH, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page + 1, pageSize, OUTCOME_MATCH, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: selectedOutcome === OUTCOME_MATCH && hasMore,
    ...QUERY_OPTIONS,
  });

  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page + 1, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: selectedOutcome === OUTCOME_MISMATCH && hasMore,
    ...QUERY_OPTIONS,
  });

  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_ERROR, tenant, user, newEngine, from?.toISOString() ?? null, to?.toISOString() ?? null],
    queryFn: async () => {
      const result = await fetchSampledQueries(page + 1, pageSize, OUTCOME_ERROR, tenant, user, newEngine, from ?? undefined, to ?? undefined);
      if (result.error) throw result.error;
      return result.data;
    },
    enabled: selectedOutcome === OUTCOME_ERROR && hasMore,
    ...QUERY_OPTIONS,
  });

  // Determine which data source to use
  const currentData = useMemo(() => {
    switch (selectedOutcome) {
      case OUTCOME_MATCH:
        return matchQuery.data || mainQuery.data;
      case OUTCOME_MISMATCH:
        return mismatchQuery.data || mainQuery.data;
      case OUTCOME_ERROR:
        return errorQuery.data || mainQuery.data;
      default:
        return mainQuery.data;
    }
  }, [selectedOutcome, matchQuery.data, mismatchQuery.data, errorQuery.data, mainQuery.data]);

  return {
    data: currentData,
    isLoading: mainQuery.isLoading,
    error: mainQuery.error,
    refetch: mainQuery.refetch,
    hasMore,
    traceId: currentTraceId,
  };
}