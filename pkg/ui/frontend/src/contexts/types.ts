import { Cluster } from "@/types/cluster";

export interface ClusterContextValue {
  cluster: Cluster | null;
  error: string | null;
  isLoading: boolean;
  refresh: () => Promise<void>;
}
