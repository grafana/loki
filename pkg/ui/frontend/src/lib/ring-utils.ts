import { formatDistanceToNowStrict, formatISO } from "date-fns";
import { cn } from "./utils";

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
