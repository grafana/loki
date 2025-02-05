import { Cluster } from "@/types/cluster";

export interface ClusterContextValue {
  cluster: Cluster | null;
  error: string | null;
  isLoading: boolean;
  refresh: () => Promise<void>;
}

export interface BreadcrumbItem {
  title: string;
  path: string;
}

export interface BreadcrumbContextType {
  items: BreadcrumbItem[];
  setBreadcrumb: (items: BreadcrumbItem[]) => void;
}
