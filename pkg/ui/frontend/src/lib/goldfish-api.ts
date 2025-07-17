import { SampledQuery, GoldfishApiResponse } from "@/types/goldfish";

export async function fetchSampledQueries(
  page: number = 1,
  pageSize: number = 20
): Promise<GoldfishApiResponse> {
  const response = await fetch(`/ui/api/v1/goldfish/queries?page=${page}&pageSize=${pageSize}`);
  if (!response.ok) {
    throw new Error(`Failed to fetch sampled queries: ${response.statusText}`);
  }
  return response.json();
}