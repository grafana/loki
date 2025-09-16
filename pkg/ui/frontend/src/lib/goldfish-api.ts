import { GoldfishAPIResponse } from "@/types/goldfish";
import { absolutePath } from "../util";
import { createTraceContext, createTraceHeaders, extractTraceId } from "./tracing";

export interface FetchResult<T> {
  data?: T;
  traceId: string;
  error?: Error;
}

export async function fetchSampledQueries(
  page: number = 1,
  pageSize: number = 20,
  tenant?: string,
  user?: string,
  newEngine?: boolean,
  from?: Date,
  to?: Date
): Promise<FetchResult<GoldfishAPIResponse>> {
  const params = new URLSearchParams({
    page: page.toString(),
    pageSize: pageSize.toString(),
  });
  
  if (tenant && tenant !== "all") {
    params.append("tenant", tenant);
  }
  
  if (user && user !== "all") {
    params.append("user", user);
  }
  
  if (newEngine !== undefined) {
    params.append("newEngine", newEngine.toString());
  }
  
  if (from) {
    params.append("from", from.toISOString());
  }
  
  if (to) {
    params.append("to", to.toISOString());
  }
  
  // Create trace context for this request
  const traceContext = createTraceContext();
  const traceHeaders = createTraceHeaders(
    traceContext.traceId,
    traceContext.spanId,
    traceContext.parentSpanId
  );
  
  try {
    const response = await fetch(`${absolutePath('/api/v1/goldfish/queries')}?${params}`, {
      headers: traceHeaders,
    });
    
    // Extract trace ID from response (might be different if backend generates its own)
    const responseTraceId = extractTraceId(response, null) || traceContext.traceId;
    
    if (!response.ok) {
      const errorText = await response.text();
      let errorMessage = `Failed to fetch sampled queries: ${response.statusText}`;
      
      try {
        const errorJson = JSON.parse(errorText);
        errorMessage = errorJson.error || errorMessage;
      } catch {
        // If not JSON, use the text as-is
        if (errorText) {
          errorMessage = errorText;
        }
      }
      
      return {
        traceId: responseTraceId,
        error: new Error(errorMessage),
      };
    }
    
    const data = await response.json();
    return {
      data,
      traceId: responseTraceId,
    };
  } catch (error) {
    // For network errors, timeouts, etc., we still have the trace ID
    return {
      traceId: traceContext.traceId,
      error: error instanceof Error ? error : new Error(String(error)),
    };
  }
}
