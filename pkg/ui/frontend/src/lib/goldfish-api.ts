import { SampledQuery, GoldfishApiResponse } from "@/types/goldfish";

// Mock data generator for development
function generateMockSampledQuery(index: number): SampledQuery {
  const baseTime = new Date();
  baseTime.setMinutes(baseTime.getMinutes() - index * 5);
  
  const queryTypes = ["range", "instant", "series", "labels"];
  const statusCodes = [200, 200, 200, 200, 500, 429]; // Mostly successful
  
  const sampleQueries = [
    `{app="loki", level="error"} |= "timeout" | json | line_format "{{.message}}"`,
    `{job="varlogs"} |~ "error|ERROR" | json | unwrap duration | __error__=""`,
    `sum by (level) (count_over_time({job="grafana"}[5m]))`,
    `{namespace="production"} | pattern "<_> <level> <_>" | level="error"`,
    `rate({app="frontend"} |= "failed to" [1m])`,
    `{cluster="us-central1", job="ingester"} | logfmt | duration > 10s`,
  ];
  
  // Generate somewhat realistic performance differences
  const cellAExecTime = Math.floor(Math.random() * 2000) + 500; // 500-2500ms
  const cellBExecTime = Math.floor(cellAExecTime * (0.5 + Math.random() * 1)); // 50%-150% of A
  
  const bytesProcessed = Math.floor(Math.random() * 1000000000) + 1000000; // 1MB-1GB
  const linesProcessed = Math.floor(bytesProcessed / 100);
  const entriesReturned = Math.floor(Math.random() * 10000);
  
  // Determine if this query should match (60% match rate)
  const shouldMatch = Math.random() > 0.4;
  const responseHash = Math.random().toString(36).substr(2, 16);
  
  // Most queries should succeed
  const isSuccessful = Math.random() > 0.15;
  const statusCode = isSuccessful ? 200 : statusCodes[Math.floor(Math.random() * statusCodes.length)];
  
  return {
    correlationId: `corr-${Math.random().toString(36).substr(2, 9)}`,
    tenantId: `tenant-${Math.floor(Math.random() * 10) + 1}`,
    query: sampleQueries[index % sampleQueries.length],
    queryType: queryTypes[index % queryTypes.length],
    startTime: new Date(baseTime.getTime() - 3600000).toISOString(), // 1 hour before
    endTime: baseTime.toISOString(),
    stepDuration: index % 3 === 0 ? 60000 : null, // Some queries have steps
    
    cellAExecTimeMs: cellAExecTime,
    cellBExecTimeMs: cellBExecTime,
    cellAQueueTimeMs: Math.floor(Math.random() * 100),
    cellBQueueTimeMs: Math.floor(Math.random() * 100),
    cellABytesProcessed: bytesProcessed,
    cellBBytesProcessed: shouldMatch ? bytesProcessed : Math.floor(bytesProcessed * (0.9 + Math.random() * 0.2)),
    cellALinesProcessed: linesProcessed,
    cellBLinesProcessed: shouldMatch ? linesProcessed : Math.floor(linesProcessed * (0.9 + Math.random() * 0.2)),
    cellABytesPerSecond: Math.floor(bytesProcessed / (cellAExecTime / 1000)),
    cellBBytesPerSecond: Math.floor(bytesProcessed / (cellBExecTime / 1000)),
    cellALinesPerSecond: Math.floor(linesProcessed / (cellAExecTime / 1000)),
    cellBLinesPerSecond: Math.floor(linesProcessed / (cellBExecTime / 1000)),
    cellAEntriesReturned: entriesReturned,
    cellBEntriesReturned: shouldMatch ? entriesReturned : Math.floor(Math.random() * 10000),
    cellASplits: Math.floor(Math.random() * 20) + 1,
    cellBSplits: Math.floor(Math.random() * 20) + 1,
    cellAShards: Math.floor(Math.random() * 64) + 1,
    cellBShards: Math.floor(Math.random() * 64) + 1,
    
    cellAResponseHash: responseHash,
    cellBResponseHash: shouldMatch ? responseHash : Math.random().toString(36).substr(2, 16),
    cellAResponseSize: Math.floor(Math.random() * 100000) + 1000,
    cellBResponseSize: Math.floor(Math.random() * 100000) + 1000,
    cellAStatusCode: statusCode,
    cellBStatusCode: shouldMatch ? statusCode : (isSuccessful ? 200 : statusCodes[Math.floor(Math.random() * statusCodes.length)]),
    
    sampledAt: baseTime.toISOString(),
    createdAt: baseTime.toISOString(),
  };
}

// Mock API function - will be replaced with real API call later
export async function fetchSampledQueries(
  page: number = 1,
  pageSize: number = 20
): Promise<GoldfishApiResponse> {
  // Simulate API delay
  await new Promise(resolve => setTimeout(resolve, 500));
  
  const total = 100; // Total mock queries
  const queries: SampledQuery[] = [];
  
  const startIndex = (page - 1) * pageSize;
  const endIndex = Math.min(startIndex + pageSize, total);
  
  for (let i = startIndex; i < endIndex; i++) {
    queries.push(generateMockSampledQuery(i));
  }
  
  return {
    queries,
    total,
    page,
    pageSize,
  };
}

// Future real API implementation would look like:
// export async function fetchSampledQueries(
//   page: number = 1,
//   pageSize: number = 20
// ): Promise<GoldfishApiResponse> {
//   const response = await fetch(`/ui/api/v1/goldfish/queries?page=${page}&pageSize=${pageSize}`);
//   if (!response.ok) {
//     throw new Error(`Failed to fetch sampled queries: ${response.statusText}`);
//   }
//   return response.json();
// }