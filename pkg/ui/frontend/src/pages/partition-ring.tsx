import { useState, useCallback, useMemo } from "react";
import { usePartitionRing } from "@/hooks/use-partition-ring";
import {
  PartitionRingTable,
  SortField,
} from "@/components/ring/partition-ring-table";
import { getStateColors, parseZoneFromOwner } from "@/lib/ring-utils";
import { useToast } from "@/hooks/use-toast";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { RefreshLoop } from "@/components/common/refresh-loop";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { Loader2, ArrowRightCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PartitionStateDistributionChart } from "@/components/ring/partition-state-distribution-chart";
import { PartitionRingFilters } from "@/components/ring/partition-ring-filters";
import { BaseRing } from "./base-ring";

const STATE_OPTIONS = [
  { value: 1, label: "Pending" },
  { value: 2, label: "Active" },
  { value: 3, label: "Inactive" },
  { value: 4, label: "Deleted" },
] as const;

export default function PartitionRing() {
  const [selectedPartitions, setSelectedPartitions] = useState<Set<number>>(
    new Set()
  );
  const [sortField, setSortField] = useState<SortField>("id");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");
  const [idFilter, setIdFilter] = useState<string[]>([]);
  const [stateFilter, setStateFilter] = useState<string[]>([]);
  const [zoneFilter, setZoneFilter] = useState<string[]>([]);
  const [ownerFilter, setOwnerFilter] = useState("");
  const [isStateChangeLoading, setIsStateChangeLoading] = useState(false);
  const [selectedNewState, setSelectedNewState] = useState<string>();
  const [isStateChangeDialogOpen, setIsStateChangeDialogOpen] = useState(false);
  const { toast } = useToast();

  const {
    partitions,
    error: partitionError,
    isLoading: isPartitionsLoading,
    fetchPartitions,
    changePartitionState,
    uniqueStates,
    uniqueZones,
  } = usePartitionRing({ isPaused: selectedPartitions.size > 0 });

  // Denormalize partitions by owner
  const denormalizedPartitions = useMemo(() => {
    return partitions.flatMap((partition) =>
      partition.owner_ids.map((owner) => ({
        ...partition,
        owner_id: owner,
        owner_ids: [owner],
        zone: parseZoneFromOwner(owner),
      }))
    );
  }, [partitions]);

  // Handle sorting
  const handleSort = useCallback((field: SortField) => {
    setSortField((currentField) => {
      if (currentField === field) {
        setSortDirection((currentDirection) =>
          currentDirection === "asc" ? "desc" : "asc"
        );
        return field;
      }
      setSortDirection("asc");
      return field;
    });
  }, []);

  // Handle partition selection
  const togglePartition = useCallback((partitionId: number) => {
    setSelectedPartitions((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(partitionId)) {
        newSet.delete(partitionId);
      } else {
        newSet.add(partitionId);
      }
      return newSet;
    });
  }, []);

  // Filter partitions
  const filteredPartitions = useMemo(() => {
    return denormalizedPartitions.filter((partition) => {
      const matchesId =
        idFilter.length === 0 || idFilter.includes(partition.id.toString());
      const matchesState =
        stateFilter.length === 0 ||
        stateFilter.includes(partition.state.toString());
      const matchesZone =
        zoneFilter.length === 0 || zoneFilter.includes(partition.zone);
      const matchesOwner = ownerFilter
        ? partition.owner_id.toLowerCase().includes(ownerFilter.toLowerCase())
        : true;

      return matchesId && matchesState && matchesZone && matchesOwner;
    });
  }, [denormalizedPartitions, idFilter, stateFilter, zoneFilter, ownerFilter]);

  // Get selected partition details
  const selectedPartitionDetails = useMemo(
    () =>
      denormalizedPartitions.filter((partition) =>
        selectedPartitions.has(partition.id)
      ),
    [denormalizedPartitions, selectedPartitions]
  );

  // Handle state change
  const handleStateChange = useCallback(async () => {
    if (selectedPartitions.size === 0 || !selectedNewState) return;

    try {
      setIsStateChangeLoading(true);
      const newState = parseInt(selectedNewState, 10);
      const { success, total } = await changePartitionState(
        selectedPartitionDetails.map((p) => p.id),
        selectedNewState
      );

      if (success > 0 && total === success) {
        toast({
          title: "State Change Success",
          description: `Successfully changed state for ${success} partition${
            success !== 1 ? "s" : ""
          } to ${STATE_OPTIONS.find((opt) => opt.value === newState)?.label}`,
        });
        await fetchPartitions();
      } else if (success < total) {
        toast({
          title: "State Change Failed",
          variant: "destructive",
          description: `Failed to change state for ${
            total - success
          } partition${total - success !== 1 ? "s" : ""}.`,
        });
      }

      setSelectedPartitions(new Set());
      setSelectedNewState(undefined);
    } catch (err) {
      toast({
        title: "Error",
        description:
          "An unexpected error occurred while changing partition states.",
      });
    } finally {
      setIsStateChangeLoading(false);
      setIsStateChangeDialogOpen(false);
    }
  }, [
    selectedPartitions,
    selectedNewState,
    selectedPartitionDetails,
    changePartitionState,
    fetchPartitions,
    toast,
  ]);

  // Table props
  const tableProps = {
    partitions: filteredPartitions,
    selectedPartitions,
    onSelectPartition: togglePartition,
    sortField,
    sortDirection,
    onSort: handleSort,
    onStateChange: handleStateChange,
  };

  if (partitionError) {
    return <BaseRing error={partitionError} ringName={"partition-ingester"} />;
  }

  return (
    <div className="container space-y-6 p-6">
      <Card>
        <CardHeader>
          <div className="grid grid-cols-[1fr_auto] gap-8">
            <div className="space-y-6">
              <div>
                <CardTitle className="text-3xl font-semibold tracking-tight">
                  Partition Ring Members
                </CardTitle>
                <p className="text-sm text-muted-foreground mt-1">
                  View and manage partition ring instances with their current
                  status and configuration
                </p>
              </div>
              <div className="flex items-center justify-between min-h-[32px]">
                <RefreshLoop
                  onRefresh={fetchPartitions}
                  isPaused={selectedPartitions.size > 0}
                  isLoading={isPartitionsLoading}
                />
                {selectedPartitions.size > 0 && (
                  <div className="flex items-center gap-4">
                    <span className="text-sm text-muted-foreground">
                      {selectedPartitions.size} partition
                      {selectedPartitions.size !== 1 ? "s" : ""} selected
                    </span>
                    <Select
                      value={selectedNewState}
                      onValueChange={setSelectedNewState}
                    >
                      <SelectTrigger className="w-[160px]">
                        <SelectValue placeholder="Select new state" />
                      </SelectTrigger>
                      <SelectContent>
                        {STATE_OPTIONS.map((option) => (
                          <SelectItem
                            key={option.value}
                            value={option.value.toString()}
                          >
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Button
                      onClick={() => setIsStateChangeDialogOpen(true)}
                      disabled={isStateChangeLoading || !selectedNewState}
                      size="sm"
                      variant="outline"
                    >
                      {isStateChangeLoading && (
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      )}
                      Change State
                    </Button>
                  </div>
                )}
              </div>
            </div>
            <div className="flex items-center">
              <div className="w-[250px]">
                <PartitionStateDistributionChart partitions={partitions} />
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          <PartitionRingFilters
            idFilter={idFilter}
            onIdFilterChange={setIdFilter}
            stateFilter={stateFilter}
            onStateFilterChange={setStateFilter}
            zoneFilter={zoneFilter}
            onZoneFilterChange={setZoneFilter}
            ownerFilter={ownerFilter}
            onOwnerFilterChange={setOwnerFilter}
            uniqueStates={uniqueStates}
            uniqueZones={uniqueZones}
            partitions={partitions}
          />
          <div className="rounded-md border bg-card">
            <PartitionRingTable {...tableProps} />
          </div>
        </CardContent>
      </Card>

      <Dialog
        open={isStateChangeDialogOpen}
        onOpenChange={setIsStateChangeDialogOpen}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm State Change</DialogTitle>
            <DialogDescription>
              Are you sure you want to change the state of these partitions?
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-[300px] overflow-y-auto">
            <div className="space-y-2">
              {Array.from(
                new Set(selectedPartitionDetails.map((p) => p.id))
              ).map((partitionId) => {
                const partition = partitions.find((p) => p.id === partitionId);
                if (!partition) return null;
                return (
                  <div
                    key={partitionId}
                    className="flex items-center justify-between p-2 rounded-md bg-muted"
                  >
                    <div className="flex items-center gap-2">
                      <span className="font-medium">
                        Partition {partitionId}
                      </span>
                      <span
                        className={cn(
                          "inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium",
                          getStateColors(partition.state)
                        )}
                      >
                        {
                          STATE_OPTIONS.find(
                            (opt) => opt.value === partition.state
                          )?.label
                        }
                      </span>
                      <ArrowRightCircle className="h-4 w-4 text-muted-foreground" />
                      <span
                        className={cn(
                          "inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium",
                          getStateColors(parseInt(selectedNewState || "0", 10))
                        )}
                      >
                        {
                          STATE_OPTIONS.find(
                            (opt) =>
                              opt.value === parseInt(selectedNewState!, 10)
                          )?.label
                        }
                      </span>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setIsStateChangeDialogOpen(false)}
              disabled={isStateChangeLoading}
            >
              Cancel
            </Button>
            <Button onClick={handleStateChange} disabled={isStateChangeLoading}>
              {isStateChangeLoading ? "Changing States..." : "Confirm Changes"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
