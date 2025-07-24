import { SampledQuery, OutcomeFilter, OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR } from "@/types/goldfish";

export function getOutcomeFromQuery(query: SampledQuery): OutcomeFilter {
  const status = query.comparisonStatus?.toLowerCase();
  
  switch (status) {
    case "match":
      return OUTCOME_MATCH;
    case "mismatch":
      return OUTCOME_MISMATCH;
    case "error":
      return OUTCOME_ERROR;
    default:
      return OUTCOME_ALL;
  }
}

export function filterQueriesByOutcome(queries: SampledQuery[], outcome: OutcomeFilter): SampledQuery[] {
  if (outcome === OUTCOME_ALL) {
    return queries;
  }
  
  return queries.filter(query => getOutcomeFromQuery(query) === outcome);
}