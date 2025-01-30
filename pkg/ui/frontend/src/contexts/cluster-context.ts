import { createContext } from "react";
import { ClusterContextValue } from "./types";

export const ClusterContext = createContext<ClusterContextValue | undefined>(
  undefined
);
