import { GoldfishAPIResponse, OutcomeFilter, OUTCOME_ALL } from "@/types/goldfish";
import { absolutePath } from "../util";

export async function fetchSampledQueries(
  page: number = 1,
  pageSize: number = 20,
  outcome?: OutcomeFilter
): Promise<GoldfishAPIResponse> {
  const params = new URLSearchParams({
    page: page.toString(),
    pageSize: pageSize.toString(),
  });
  
  if (outcome && outcome !== OUTCOME_ALL) {
    params.append("outcome", outcome);
  }
  
  const response = await fetch(`${absolutePath('/api/v1/goldfish/queries')}?${params}`);
  if (!response.ok) {
    throw new Error(`Failed to fetch sampled queries: ${response.statusText}`);
  }
  return response.json();
}
