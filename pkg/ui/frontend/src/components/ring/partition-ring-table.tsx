import { useMemo, useEffect } from "react";
import { Link } from "react-router-dom";
import { PartitionInstance } from "@/types/ring";
import {
  formatTimestamp,
  formatRelativeTime,
  getZoneColors,
  formatBytes,
} from "@/lib/ring-utils";
import { cn } from "@/lib/utils";
import { Checkbox } from "@/components/ui/checkbox";
import { ArrowRightCircle, ArrowUpCircle, ArrowDownCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { DataTableColumnHeader } from "@/components/common/data-table-column-header";

export type SortField =
  | "id"
  | "state"
  | "owner"
  | "timestamp"
  | "zone"
  | "uncompressed_rate"
  | "compressed_rate";

interface SelectAllCheckboxProps {
  allPartitions: PartitionInstance[];
  selectedIds: Set<number>;
  onChange: (selectedIds: Set<number>) => void;
}

function SelectAllCheckbox({
  allPartitions,
  selectedIds,
  onChange,
}: SelectAllCheckboxProps) {
  // Get unique partition IDs from all partitions
  const uniquePartitionIds = useMemo(() => {
    return Array.from(new Set(allPartitions.map((p) => p.id)));
  }, [allPartitions]);

  const allSelected = uniquePartitionIds.every((id) => selectedIds.has(id));

  const handleChange = () => {
    if (allSelected) {
      // Unselect all partitions
      onChange(new Set());
    } else {
      // Select all unique partitions
      onChange(new Set(uniquePartitionIds));
    }
  };

  return (
    <Checkbox
      checked={uniquePartitionIds.length > 0 && allSelected}
      onCheckedChange={handleChange}
      aria-label="Select all partitions"
    />
  );
}

function getStateColors(state: number): string {
  switch (state) {
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

interface RateMetrics {
  uncompressedRate: number;
  compressedRate: number;
}

interface NodeMetrics {
  [nodeId: string]: RateMetrics;
}

interface PartitionRingTableProps {
  partitions: PartitionInstance[];
  selectedPartitions: Set<number>;
  onSelectPartition: (id: number) => void;
  sortField: SortField;
  sortDirection: "asc" | "desc";
  onSort: (field: SortField) => void;
  onStateChange: (partitionIds: number[], newState: number) => void;
  previousPartitions?: PartitionInstance[];
}

const STATE_OPTIONS = [
  { value: 1, label: "Pending" },
  { value: 2, label: "Active" },
  { value: 3, label: "Inactive" },
  { value: 4, label: "Deleted" },
];

function getRateTrend(
  current: number | undefined,
  previous: number | undefined
): "up" | "down" | null {
  if (current === undefined || previous === undefined) {
    return null;
  }

  // Add a small threshold to avoid showing changes for tiny fluctuations
  const threshold = 0.1; // 10% threshold
  const percentChange = Math.abs((current - previous) / previous);

  if (percentChange < threshold) {
    return null;
  }

  return current > previous ? "up" : "down";
}

function TrendIndicator({ trend }: { trend: "up" | "down" | null }) {
  if (!trend) return null;

  return trend === "up" ? (
    <ArrowUpCircle className="inline h-4 w-4 text-green-500 ml-1" />
  ) : (
    <ArrowDownCircle className="inline h-4 w-4 text-red-500 ml-1" />
  );
}

export function PartitionRingTable({
  partitions,
  selectedPartitions,
  onSelectPartition,
  sortField,
  sortDirection,
  onSort,
  previousPartitions = [],
}: PartitionRingTableProps) {
  // Create a map of previous values for quick lookup
  const previousValues = useMemo(() => {
    const values = previousPartitions.reduce((acc, partition) => {
      const key = `${partition.owner_id}-${partition.id}`;
      acc[key] = partition;
      return acc;
    }, {} as Record<string, PartitionInstance>);

    return values;
  }, [previousPartitions]);

  // Sort partitions according to the current sort field
  const sortedPartitions = useMemo(() => {
    return [...partitions].sort((a, b) => {
      let comparison = 0;
      switch (sortField) {
        case "uncompressed_rate": {
          comparison = (a.uncompressedRate || 0) - (b.uncompressedRate || 0);
          break;
        }
        case "compressed_rate": {
          comparison = (a.compressedRate || 0) - (b.compressedRate || 0);
          break;
        }
        case "id":
          comparison = a.id - b.id;
          break;
        case "state":
          comparison = a.state - b.state;
          break;
        case "owner":
          comparison = a.owner_id?.localeCompare(b.owner_id || "") || 0;
          break;
        case "zone":
          comparison = (a.zone || "").localeCompare(b.zone || "");
          break;
        case "timestamp":
          comparison =
            new Date(a.state_timestamp).getTime() -
            new Date(b.state_timestamp).getTime();
          break;
      }
      return sortDirection === "asc" ? comparison : -comparison;
    });
  }, [partitions, sortField, sortDirection]);

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="w-[50px]">
              <SelectAllCheckbox
                allPartitions={partitions}
                selectedIds={selectedPartitions}
                onChange={(newSelection) => {
                  const uniqueIds = new Set(partitions.map((p) => p.id));
                  uniqueIds.forEach((id) => {
                    if (newSelection.has(id) !== selectedPartitions.has(id)) {
                      onSelectPartition(id);
                    }
                  });
                }}
              />
            </TableHead>
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<SortField>
                title="Owner"
                field="owner"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[150px]">
              <DataTableColumnHeader<SortField>
                title="Zone"
                field="zone"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[100px]">
              <DataTableColumnHeader<SortField>
                title="Partition ID"
                field="id"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[150px]">
              <DataTableColumnHeader<SortField>
                title="State"
                field="state"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<SortField>
                title="Last Update"
                field="timestamp"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[150px]">
              <DataTableColumnHeader<SortField>
                title="Uncompressed Rate"
                field="uncompressed_rate"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[150px]">
              <DataTableColumnHeader<SortField>
                title="Compressed Rate"
                field="compressed_rate"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[100px]" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {sortedPartitions.map((partition) => {
            return (
              <TableRow key={`${partition.owner_id}-${partition.id}`}>
                <TableCell>
                  <Checkbox
                    checked={selectedPartitions.has(partition.id)}
                    onCheckedChange={() => onSelectPartition(partition.id)}
                    aria-label={`Select partition ${partition.id}`}
                  />
                </TableCell>
                <TableCell className="font-medium">
                  <Link
                    to={`/nodes/${partition.owner_id}`}
                    className="hover:underline"
                  >
                    {partition.owner_id}
                  </Link>
                </TableCell>
                <TableCell>
                  <span
                    className={cn(
                      "inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium",
                      getZoneColors(partition.zone || "")
                    )}
                  >
                    {partition.zone || "-"}
                  </span>
                </TableCell>
                <TableCell>
                  <span
                    className={cn(
                      "inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium",
                      partition.corrupted
                        ? "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200"
                        : "bg-muted"
                    )}
                    title={partition.corrupted ? "Corrupted" : undefined}
                  >
                    {partition.id}
                  </span>
                </TableCell>
                <TableCell>
                  <span
                    className={cn(
                      "inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium",
                      getStateColors(partition.state)
                    )}
                  >
                    {STATE_OPTIONS.find((opt) => opt.value === partition.state)
                      ?.label || "Unknown"}
                  </span>
                </TableCell>
                <TableCell>
                  <span
                    title={formatTimestamp(partition.state_timestamp)}
                    className="text-muted-foreground"
                  >
                    {formatRelativeTime(partition.state_timestamp)}
                  </span>
                </TableCell>
                <TableCell>
                  <span className="text-muted-foreground inline-flex items-center">
                    {formatBytes(partition.uncompressedRate || 0)}/s
                    <TrendIndicator
                      trend={getRateTrend(
                        partition.uncompressedRate,
                        partition.previousUncompressedRate
                      )}
                    />
                  </span>
                </TableCell>
                <TableCell>
                  <span className="text-muted-foreground inline-flex items-center">
                    {formatBytes(partition.compressedRate || 0)}/s
                    <TrendIndicator
                      trend={getRateTrend(
                        partition.compressedRate,
                        partition.previousCompressedRate
                      )}
                    />
                  </span>
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Link
                      to={`/nodes/${partition.owner_id}`}
                      className="hover:underline"
                    >
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-8 w-8"
                        title="View instance details"
                      >
                        <ArrowRightCircle className="h-4 w-4" />
                      </Button>
                    </Link>
                  </div>
                </TableCell>
              </TableRow>
            );
          })}
          {sortedPartitions.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="h-24 text-center">
                <div className="text-muted-foreground">No partitions found</div>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </>
  );
}
