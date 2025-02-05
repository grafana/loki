import { useContext } from "react";
import { ClusterContextValue } from "./types";
import { ClusterContext } from "./cluster-context";

export function useCluster(): ClusterContextValue {
  const context = useContext(ClusterContext);
  if (!context) {
    throw new Error("useCluster must be used within a ClusterProvider");
  }
  return context;
}
