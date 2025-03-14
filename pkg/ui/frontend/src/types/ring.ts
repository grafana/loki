export interface RingInstance {
  id: string;
  state: string;
  address: string;
  timestamp: string;
  registered_timestamp: string;
  read_only: boolean;
  read_only_updated_timestamp: string;
  zone: string;
  tokens: number[];
  ownership: string;
}

export interface RingResponse {
  shards: RingInstance[];
  now: string;
}

export const RingTypes: Record<string, string> = {
  INGESTER: "ingester",
  PARTITION_INGESTER: "partition-ingester",
  DISTRIBUTOR: "distributor",
  PATTERN_INGESTER: "pattern-ingester",
  QUERY_SCHEDULER: "query-scheduler",
  COMPACTOR: "compactor",
  RULER: "ruler",
  INDEX_GATEWAY: "index-gateway",
} as const;

export type RingType = keyof typeof RingTypes;

export interface PartitionInstance {
  id: number;
  corrupted: boolean;
  state: number;
  state_timestamp: string;
  owner_ids: string[];
  tokens: number[];
  owner_id?: string;
  zone?: string;
  uncompressedRate?: number;
  compressedRate?: number;
  previousUncompressedRate?: number;
  previousCompressedRate?: number;
}

export interface PartitionRingResponse {
  partitions: PartitionInstance[];
  now: string;
}

export const PartitionStates = {
  0: "Unknown",
  1: "Pending",
  2: "Active",
  3: "Inactive",
  4: "Deleted",
} as const;

export type PartitionState = keyof typeof PartitionStates;
