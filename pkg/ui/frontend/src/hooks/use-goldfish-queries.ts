import { useMemo } from 'react';
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
  newEngine?: boolean
) {
  // Main query always fetches all data
  const mainQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_ALL, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page, pageSize, OUTCOME_ALL, tenant, user, newEngine),
    ...QUERY_OPTIONS,
  });

  // Background queries for specific filters
  const matchQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_MATCH, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page, pageSize, OUTCOME_MATCH, tenant, user, newEngine),
    enabled: selectedOutcome === OUTCOME_MATCH,
    ...QUERY_OPTIONS,
  });

  const mismatchQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine),
    enabled: selectedOutcome === OUTCOME_MISMATCH,
    ...QUERY_OPTIONS,
  });

  const errorQuery = useQuery({
    queryKey: ['goldfish-queries', page, pageSize, OUTCOME_ERROR, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page, pageSize, OUTCOME_ERROR, tenant, user, newEngine),
    enabled: selectedOutcome === OUTCOME_ERROR,
    ...QUERY_OPTIONS,
  });

  // Calculate total pages
  const totalPages = useMemo(() => {
    return mainQuery.data ? Math.ceil(mainQuery.data.total / pageSize) : 0;
  }, [mainQuery.data, pageSize]);

  // Prefetch next page for main query
  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_ALL, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page + 1, pageSize, OUTCOME_ALL, tenant, user, newEngine),
    enabled: totalPages > 1 && page < totalPages,
    ...QUERY_OPTIONS,
  });

  // Prefetch next page for specific filter outcomes
  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_MATCH, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page + 1, pageSize, OUTCOME_MATCH, tenant, user, newEngine),
    enabled: selectedOutcome === OUTCOME_MATCH && totalPages > 1 && page < totalPages,
    ...QUERY_OPTIONS,
  });

  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page + 1, pageSize, OUTCOME_MISMATCH, tenant, user, newEngine),
    enabled: selectedOutcome === OUTCOME_MISMATCH && totalPages > 1 && page < totalPages,
    ...QUERY_OPTIONS,
  });

  useQuery({
    queryKey: ['goldfish-queries', page + 1, pageSize, OUTCOME_ERROR, tenant, user, newEngine],
    queryFn: () => fetchSampledQueries(page + 1, pageSize, OUTCOME_ERROR, tenant, user, newEngine),
    enabled: selectedOutcome === OUTCOME_ERROR && totalPages > 1 && page < totalPages,
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
    totalPages,
  };
}