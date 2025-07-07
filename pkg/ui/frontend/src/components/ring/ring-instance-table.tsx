import { Link } from "react-router-dom";
import { RingInstance } from "@/types/ring";
import {
  formatRelativeTime,
  formatTimestamp,
  getStateColors,
  getZoneColors,
} from "@/lib/ring-utils";
import { cn } from "@/lib/utils";
import { Checkbox } from "@/components/ui/checkbox";
import { ArrowRightCircle } from "lucide-react";
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
import { Progress } from "@/components/ui/progress";

export type SortField =
  | "id"
  | "state"
  | "address"
  | "zone"
  | "timestamp"
  | "ownership"
  | "tokens";

interface SelectAllCheckboxProps {
  visibleIds: string[];
  selectedIds: Set<string>;
  onChange: (selectedIds: Set<string>) => void;
}

function SelectAllCheckbox({
  visibleIds,
  selectedIds,
  onChange,
}: SelectAllCheckboxProps) {
  const allVisibleSelected = visibleIds.every((id) => selectedIds.has(id));

  const handleChange = () => {
    const visibleIdsSet = new Set(visibleIds);
    if (allVisibleSelected) {
      // Keep only the instances that are not currently visible
      onChange(
        new Set([...selectedIds].filter((id) => !visibleIdsSet.has(id)))
      );
    } else {
      // Add all visible instances to the current selection
      onChange(new Set([...selectedIds, ...visibleIds]));
    }
  };

  return (
    <Checkbox
      checked={visibleIds.length > 0 && allVisibleSelected}
      onCheckedChange={handleChange}
      aria-label="Select all visible instances"
    />
  );
}

interface RingInstanceTableProps {
  instances: RingInstance[];
  selectedInstances: Set<string>;
  onSelectInstance: (instanceId: string) => void;
  sortField: SortField;
  sortDirection: "asc" | "desc";
  onSort: (field: SortField) => void;
  showTokens?: boolean;
}

export function RingInstanceTable({
  instances,
  selectedInstances,
  onSelectInstance,
  sortField,
  sortDirection,
  onSort,
  showTokens = false,
}: RingInstanceTableProps) {
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead className="w-[50px]">
            <SelectAllCheckbox
              visibleIds={instances.map((instance) => instance.id)}
              selectedIds={selectedInstances}
              onChange={(newSelection) => {
                instances.forEach((instance) => {
                  if (
                    newSelection.has(instance.id) !==
                    selectedInstances.has(instance.id)
                  ) {
                    onSelectInstance(instance.id);
                  }
                });
              }}
            />
          </TableHead>
          <TableHead className="w-[200px]">
            <DataTableColumnHeader<SortField>
              title="ID"
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
          <TableHead>
            <DataTableColumnHeader<SortField>
              title="Address"
              field="address"
              sortField={sortField}
              sortDirection={sortDirection}
              onSort={onSort}
            />
          </TableHead>
          {showTokens && (
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<SortField>
                title="Ownership"
                field="ownership"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
          )}
          <TableHead className="w-[150px]">
            <DataTableColumnHeader<SortField>
              title="Zone"
              field="zone"
              sortField={sortField}
              sortDirection={sortDirection}
              onSort={onSort}
            />
          </TableHead>
          <TableHead className="w-[200px]">
            <DataTableColumnHeader<SortField>
              title="Last Heartbeat"
              field="timestamp"
              sortField={sortField}
              sortDirection={sortDirection}
              onSort={onSort}
            />
          </TableHead>
          <TableHead className="w-[50px]" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {instances.map((instance) => {
          const ownership = showTokens ? instance.ownership : 0;
          return (
            <TableRow key={instance.id}>
              <TableCell>
                <Checkbox
                  checked={selectedInstances.has(instance.id)}
                  onCheckedChange={() => onSelectInstance(instance.id)}
                  aria-label={`Select instance ${instance.id}`}
                />
              </TableCell>
              <TableCell className="font-medium">
                <Link to={`/nodes/${instance.id}`} className="hover:underline">
                  {instance.id}
                </Link>
              </TableCell>
              <TableCell>
                <span
                  className={cn(
                    "inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium",
                    getStateColors(instance.state)
                  )}
                >
                  {instance.state}
                </span>
              </TableCell>
              <TableCell>{instance.address}</TableCell>
              {showTokens && (
                <TableCell>
                  <div className="space-y-1">
                    <div className="flex justify-between text-xs">
                      <span>{ownership}</span>
                      <span className="text-muted-foreground">
                        {instance.tokens.length} tokens
                      </span>
                    </div>
                    <Progress
                      value={
                        typeof ownership === "number"
                          ? ownership
                          : Number(ownership.slice(0, -1))
                      }
                      className="h-2"
                    />
                  </div>
                </TableCell>
              )}
              <TableCell>
                {instance.zone ? (
                  <span
                    className={cn(
                      "inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium",
                      getZoneColors(instance.zone)
                    )}
                  >
                    {instance.zone}
                  </span>
                ) : (
                  <span className="text-muted-foreground">-</span>
                )}
              </TableCell>
              <TableCell>
                <span
                  title={formatTimestamp(instance.timestamp)}
                  className="text-muted-foreground"
                >
                  {formatRelativeTime(instance.timestamp)}
                </span>
              </TableCell>
              <TableCell>
                <Button
                  variant="ghost"
                  size="icon"
                  asChild
                  className="h-8 w-8"
                  title="View instance details"
                >
                  <Link to={`/nodes/${instance.id}`}>
                    <ArrowRightCircle className="h-4 w-4" />
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          );
        })}
        {instances.length === 0 && (
          <TableRow>
            <TableCell colSpan={7} className="h-24 text-center">
              <div className="text-muted-foreground">No instances found</div>
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  );
}
