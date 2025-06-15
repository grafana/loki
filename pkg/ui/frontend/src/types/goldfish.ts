export interface SampledQuery {
  correlationId: string;
  tenantId: string;
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
  
  sampledAt: string;
  createdAt: string;
}

export interface GoldfishApiResponse {
  queries: SampledQuery[];
  total: number;
  page: number;
  pageSize: number;
}