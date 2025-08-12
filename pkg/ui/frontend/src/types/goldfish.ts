export interface SampledQuery {
  correlationId: string;
  tenantId: string;
  user: string;
  query: string;
  queryType: string;
  startTime: string;
  endTime: string;
  stepDuration: number | null;
  
  // Performance statistics
  cellAExecTimeMs: number | null;
  cellBExecTimeMs: number | null;
  cellAQueueTimeMs: number | null;
  cellBQueueTimeMs: number | null;
  cellABytesProcessed: number | null;
  cellBBytesProcessed: number | null;
  cellALinesProcessed: number | null;
  cellBLinesProcessed: number | null;
  cellABytesPerSecond: number | null;
  cellBBytesPerSecond: number | null;
  cellALinesPerSecond: number | null;
  cellBLinesPerSecond: number | null;
  cellAEntriesReturned: number | null;
  cellBEntriesReturned: number | null;
  cellASplits: number | null;
  cellBSplits: number | null;
  cellAShards: number | null;
  cellBShards: number | null;
  
  // Response metadata
  cellAResponseHash: string | null;
  cellBResponseHash: string | null;
  cellAResponseSize: number | null;
  cellBResponseSize: number | null;
  cellAStatusCode: number | null;
  cellBStatusCode: number | null;
  
  // Trace IDs
  cellATraceID: string | null;
  cellBTraceID: string | null;
  
  // Trace ID explore links (only included when explore config is available)
  cellATraceLink?: string | null;
  cellBTraceLink?: string | null;
  
  // Logs explore links (only included when logs config is available)
  cellALogsLink?: string | null;
  cellBLogsLink?: string | null;
  
  // Query engine version tracking
  cellAUsedNewEngine: boolean;
  cellBUsedNewEngine: boolean;
  
  sampledAt: string;
  createdAt: string;
  comparisonStatus: string;
}

// Outcome filter constants
export const OUTCOME_ALL = "all" as const;
export const OUTCOME_MATCH = "match" as const;
export const OUTCOME_MISMATCH = "mismatch" as const;
export const OUTCOME_ERROR = "error" as const;

export type OutcomeFilter = typeof OUTCOME_ALL | typeof OUTCOME_MATCH | typeof OUTCOME_MISMATCH | typeof OUTCOME_ERROR;

export interface ComparisonOutcome {
  correlationId: string;
  comparisonStatus: string;
  differenceDetails: any | null;
  performanceMetrics: any | null;
  comparedAt: string;
  createdAt: string;
}

export interface GoldfishAPIResponse {
  queries: SampledQuery[];
  total: number;
  page: number;
  pageSize: number;
}
