export interface BuildInfo {
  version: string;
  revision: string;
  branch: string;
  buildUser: string;
  buildDate: string;
  goVersion: string;
}

export interface ServiceState {
  service: string;
  status: string;
}

export type NodeState =
  | "New"
  | "Starting"
  | "Running"
  | "Stopping"
  | "Terminated"
  | "Failed";

export const ALL_NODE_STATES: NodeState[] = [
  "New",
  "Starting",
  "Running",
  "Stopping",
  "Terminated",
  "Failed",
];

export interface Member {
  addr: string;
  state: string;
  isSelf: boolean;
  target: string;
  services: ServiceState[];
  build: BuildInfo;
  error?: string;
  ready?: {
    isReady: boolean;
    message: string;
  };
}

export interface Cluster {
  members: { [key: string]: Member };
}

export const ALL_VALUES_TARGET = "all-values";
