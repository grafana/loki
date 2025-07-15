import { cn } from "@/lib/utils";

interface StorageTypeIndicatorProps {
  type: string;
  className?: string;
}

const storageTypeColors: Record<string, string> = {
  // AWS related
  aws: "text-yellow-600 bg-yellow-100 dark:bg-yellow-950 dark:text-yellow-400",
  "aws-dynamo":
    "text-yellow-600 bg-yellow-100 dark:bg-yellow-950 dark:text-yellow-400",
  s3: "text-yellow-600 bg-yellow-100 dark:bg-yellow-950 dark:text-yellow-400",

  // Azure
  azure: "text-blue-600 bg-blue-100 dark:bg-blue-950 dark:text-blue-400",

  // GCP related
  gcp: "text-blue-600 bg-blue-100 dark:bg-blue-950 dark:text-blue-400",
  "gcp-columnkey":
    "text-blue-600 bg-blue-100 dark:bg-blue-950 dark:text-blue-400",
  gcs: "text-blue-600 bg-blue-100 dark:bg-blue-950 dark:text-blue-400",

  // Alibaba Cloud
  alibabacloud:
    "text-orange-600 bg-orange-100 dark:bg-orange-950 dark:text-orange-400",

  // Local storage types
  filesystem: "text-gray-600 bg-gray-100 dark:bg-gray-800 dark:text-gray-400",
  local: "text-gray-600 bg-gray-100 dark:bg-gray-800 dark:text-gray-400",

  // Database types
  boltdb:
    "text-emerald-600 bg-emerald-100 dark:bg-emerald-950 dark:text-emerald-400",
  cassandra: "text-blue-700 bg-blue-100 dark:bg-blue-950 dark:text-blue-400",
  bigtable: "text-red-600 bg-red-100 dark:bg-red-950 dark:text-red-400",
  "bigtable-hashed":
    "text-red-600 bg-red-100 dark:bg-red-950 dark:text-red-400",

  // Other cloud providers
  bos: "text-cyan-600 bg-cyan-100 dark:bg-cyan-950 dark:text-cyan-400",
  cos: "text-green-600 bg-green-100 dark:bg-green-950 dark:text-green-400",
  swift:
    "text-orange-600 bg-orange-100 dark:bg-orange-950 dark:text-orange-400",

  // Special types
  inmemory:
    "text-purple-600 bg-purple-100 dark:bg-purple-950 dark:text-purple-400",
  "grpc-store":
    "text-indigo-600 bg-indigo-100 dark:bg-indigo-950 dark:text-indigo-400",
};

export function StorageTypeIndicator({
  type,
  className,
}: StorageTypeIndicatorProps) {
  const normalizedType = type.toLowerCase();
  const colorClasses =
    storageTypeColors[normalizedType] ||
    "text-gray-600 bg-gray-100 dark:bg-gray-800 dark:text-gray-400";

  return (
    <span
      className={cn(
        "inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium",
        colorClasses,
        className
      )}
    >
      {normalizedType}
    </span>
  );
}
