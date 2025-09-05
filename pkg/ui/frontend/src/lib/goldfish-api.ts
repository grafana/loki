import { GoldfishAPIResponse, OutcomeFilter, OUTCOME_ALL } from "@/types/goldfish";
import { absolutePath } from "../util";

export async function fetchSampledQueries(
  page: number = 1,
  pageSize: number = 20,
  outcome?: OutcomeFilter,
  tenant?: string,
  user?: string,
  newEngine?: boolean
): Promise<GoldfishAPIResponse> {
  const params = new URLSearchParams({
    page: page.toString(),
    pageSize: pageSize.toString(),
  });
  
  if (outcome && outcome !== OUTCOME_ALL) {
    params.append("outcome", outcome);
  }
  
  if (tenant && tenant !== "all") {
    params.append("tenant", tenant);
  }
  
  if (user && user !== "all") {
    params.append("user", user);
  }
  
  if (newEngine !== undefined) {
    params.append("newEngine", newEngine.toString());
  }
  
  const response = await fetch(`${absolutePath('/api/v1/goldfish/queries')}?${params}`);
  if (!response.ok) {
    throw new Error(`Failed to fetch sampled queries: ${response.statusText}`);
  }
  return response.json();
}
