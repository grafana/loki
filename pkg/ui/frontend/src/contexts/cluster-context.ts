import { createContext } from "react";
import { ClusterContextValue } from "./types";

const initialContextValue: ClusterContextValue = {
  cluster: null,
  error: null,
  isLoading: true,
  refresh: () => Promise.resolve(),
};

export const ClusterContext =
  createContext<ClusterContextValue>(initialContextValue);
