import { RingType, RingTypes } from "@/types/ring";
import { formatDistanceToNowStrict, formatISO } from "date-fns";
import { findNodeName, hasService } from "./utils";
import { absolutePath } from "@/util";

export function formatRelativeTime(timestamp: string) {
  const date = new Date(timestamp);
  return `${formatDistanceToNowStrict(date)} ago`;
}

export function formatTimestamp(timestamp: string) {
  const date = new Date(timestamp);
  return formatISO(date, { format: "extended" });
}

export function getStateColors(state: string | number): string {
  const numericState = typeof state === "string" ? parseInt(state, 10) : state;
  switch (numericState) {
    case 2: // Active
      return "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200";
    case 1: // Pending
      return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
    case 3: // Inactive
      return "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200";
    case 4: // Deleted
      return "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200";
    default: // Unknown
      return "bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200";
  }
}

export function getZoneColors(zone: string) {
  // Create a consistent hash of the zone name to always get the same color for the same zone
  const hash = zone.split("").reduce((acc, char) => {
    return char.charCodeAt(0) + ((acc << 5) - acc);
  }, 0);

  // Using only colors not used in state indicators
  // Avoiding: green, yellow, blue, purple, red, gray
  const colors = [
    "bg-rose-100 text-rose-800 dark:bg-rose-900 dark:text-rose-200",
    "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200",
    "bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200",
    "bg-lime-100 text-lime-800 dark:bg-lime-900 dark:text-lime-200",
    "bg-teal-100 text-teal-800 dark:bg-teal-900 dark:text-teal-200",
    "bg-cyan-100 text-cyan-800 dark:bg-cyan-900 dark:text-cyan-200",
    "bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-200",
    "bg-fuchsia-100 text-fuchsia-800 dark:bg-fuchsia-900 dark:text-fuchsia-200",
  ];

  const index = Math.abs(hash) % colors.length;
  return colors[index];
}

export function parseZoneFromOwner(owner: string): string {
  const parts = owner.split("-");
  return parts.length >= 3 ? parts[parts.length - 2] : "";
}

export function getFirstZoneFromOwners(owners: string[]): string {
  if (!owners.length) return "";
  return parseZoneFromOwner(owners[0]);
}

export function formatBytes(bytes: number): string {
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let value = bytes;
  let unitIndex = 0;

  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex++;
  }

  return `${value.toFixed(1)} ${units[unitIndex]}`;
}

interface Member {
  services?: Array<{ service: string }>;
}

// Helper function to check if a service exists in any member
export const clusterSupportService = (
  members: Record<string, Member>,
  serviceName: string
): boolean => {
  return Object.values(members).some((member) =>
    hasService(member, serviceName)
  );
};

export const ServiceNames = {
  ingester: "ingester",
  "partition-ring": "partition-ring",
  distributor: "distributor",
  "pattern-ingester": "pattern-ingester",
  "query-scheduler": "query-scheduler",
  compactor: "compactor",
  ruler: "ruler",
  "index-gateway": "index-gateway",
};

export const RingServices: Record<
  string,
  { title: string; ringName: RingType; ringPath: string; needsTokens: boolean }
> = {
  ingester: {
    title: "Ingester",
    ringName: RingTypes.INGESTER,
    ringPath: "/ring",
    needsTokens: true,
  },
  "partition-ring": {
    title: "Partition Ingester",
    ringName: RingTypes.PARTITION_INGESTER,
    ringPath: "/partition-ring",
    needsTokens: true,
  },
  distributor: {
    title: "Distributor",
    ringName: RingTypes.DISTRIBUTOR,
    ringPath: "/distributor/ring",
    needsTokens: false,
  },
  "pattern-ingester": {
    title: "Pattern Ingester",
    ringName: RingTypes.PATTERN_INGESTER,
    ringPath: "/pattern/ring",
    needsTokens: true,
  },
  "query-scheduler": {
    title: "Scheduler",
    ringName: RingTypes.QUERY_SCHEDULER,
    ringPath: "/scheduler/ring",
    needsTokens: false,
  },
  compactor: {
    title: "Compactor",
    ringName: RingTypes.COMPACTOR,
    ringPath: "/compactor/ring",
    needsTokens: false,
  },
  ruler: {
    title: "Ruler",
    ringName: RingTypes.RULER,
    ringPath: "/ruler/ring",
    needsTokens: true,
  },
  "index-gateway": {
    title: "Index Gateway",
    ringName: RingTypes.INDEX_GATEWAY,
    ringPath: "/indexgateway/ring",
    needsTokens: true,
  },
};

function findServiceName(ringType: RingType) {
  return Object.keys(RingServices).find(
    (key) => RingServices[key].ringName === ringType
  );
}

// Function to determine if a ring type needs tokens
export function needsTokens(ringName: RingType | undefined): boolean {
  if (!ringName) return false;
  const serviceName = findServiceName(ringName);
  if (!serviceName) return false;
  return RingServices[serviceName].needsTokens;
}

// Helper function to get available rings based on cluster services
export const getAvailableRings = (
  members: Record<string, Member>
): Array<{ title: string; url: string }> => {
  const rings: Array<{ title: string; url: string }> = [];

  if (!members) return rings;

  // loop through services type an push to rigns

  for (const service in RingServices) {
    if (clusterSupportService(members, service)) {
      rings.push({
        title: RingServices[service].title,
        url: `/rings/${RingServices[service].ringName}`,
      });
    }
  }
  return rings;
};

// Utility function to get ring proxy path
export function getRingProxyPath(
  members: Record<string, Member> | undefined,
  ringName: RingType
): string {
  if (!members) return "";
  if (!ringName) return "";

  const serviceName = findServiceName(ringName);
  if (!serviceName) return "";
  // Find the first member that has the serviceName
  const nodeName = findNodeName(members, serviceName);
  if (!nodeName) return "";

  const proxyPath = absolutePath(`/api/v1/proxy/${nodeName}`);
  const ringPath = RingServices[serviceName].ringPath;
  const tokensParam = RingServices[serviceName].needsTokens
    ? "?tokens=true"
    : "";
  return `${proxyPath}${ringPath}${tokensParam}`;
}
