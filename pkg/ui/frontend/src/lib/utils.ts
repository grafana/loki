import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB", "PB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

interface Member {
  services?: Array<{ service: string }>;
}

export function findNodeName(
  members: Record<string, Member> | undefined,
  serviceName: string
) {
  if (!members) return null;
  return Object.keys(members).find((key) =>
    hasService(members[key], serviceName)
  );
}

export function hasService(member: Member, serviceName: string): boolean {
  return (
    member.services?.some((service) => service.service === serviceName) ?? false
  );
}
