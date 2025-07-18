import { getOutcomeFromQuery, filterQueriesByOutcome } from './goldfish-utils';
import { SampledQuery, OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR } from '@/types/goldfish';

describe('goldfish-utils', () => {
  describe('getOutcomeFromQuery', () => {
    it('maps "match" comparisonStatus to OUTCOME_MATCH', () => {
      const query = { comparisonStatus: 'match' } as SampledQuery;
      expect(getOutcomeFromQuery(query)).toBe(OUTCOME_MATCH);
    });

    it('maps "mismatch" comparisonStatus to OUTCOME_MISMATCH', () => {
      const query = { comparisonStatus: 'mismatch' } as SampledQuery;
      expect(getOutcomeFromQuery(query)).toBe(OUTCOME_MISMATCH);
    });

    it('maps "error" comparisonStatus to OUTCOME_ERROR', () => {
      const query = { comparisonStatus: 'error' } as SampledQuery;
      expect(getOutcomeFromQuery(query)).toBe(OUTCOME_ERROR);
    });

    it('maps unknown comparisonStatus to OUTCOME_ALL', () => {
      const query = { comparisonStatus: 'unknown' } as SampledQuery;
      expect(getOutcomeFromQuery(query)).toBe(OUTCOME_ALL);
    });

    it('handles case insensitive matching', () => {
      const query = { comparisonStatus: 'MATCH' } as SampledQuery;
      expect(getOutcomeFromQuery(query)).toBe(OUTCOME_MATCH);
    });

    it('handles undefined comparisonStatus', () => {
      const query = { comparisonStatus: undefined } as unknown as SampledQuery;
      expect(getOutcomeFromQuery(query)).toBe(OUTCOME_ALL);
    });
  });

  describe('filterQueriesByOutcome', () => {
    const matchQuery = { comparisonStatus: 'match', correlationId: '1' } as SampledQuery;
    const mismatchQuery = { comparisonStatus: 'mismatch', correlationId: '2' } as SampledQuery;
    const errorQuery = { comparisonStatus: 'error', correlationId: '3' } as SampledQuery;
    const queries = [matchQuery, mismatchQuery, errorQuery];

    it('returns all queries when outcome is OUTCOME_ALL', () => {
      const result = filterQueriesByOutcome(queries, OUTCOME_ALL);
      expect(result).toEqual(queries);
    });

    it('returns only matching queries when outcome is OUTCOME_MATCH', () => {
      const result = filterQueriesByOutcome(queries, OUTCOME_MATCH);
      expect(result).toEqual([matchQuery]);
    });

    it('returns only mismatching queries when outcome is OUTCOME_MISMATCH', () => {
      const result = filterQueriesByOutcome(queries, OUTCOME_MISMATCH);
      expect(result).toEqual([mismatchQuery]);
    });

    it('returns only error queries when outcome is OUTCOME_ERROR', () => {
      const result = filterQueriesByOutcome(queries, OUTCOME_ERROR);
      expect(result).toEqual([errorQuery]);
    });

    it('returns empty array when no queries match the outcome', () => {
      const allMatchQueries = [matchQuery, matchQuery];
      const result = filterQueriesByOutcome(allMatchQueries, OUTCOME_ERROR);
      expect(result).toEqual([]);
    });

    it('handles empty input array', () => {
      const result = filterQueriesByOutcome([], OUTCOME_MATCH);
      expect(result).toEqual([]);
    });
  });
});