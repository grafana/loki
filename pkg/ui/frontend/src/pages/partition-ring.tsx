import { useState, useCallback, useMemo, useEffect, useRef } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Loader2, ArrowRightCircle, X } from "lucide-react";
import { usePartitionRing } from "@/hooks/use-partition-ring";
import { useRateNodeMetrics } from "@/hooks/use-rate-node-metrics";
import {
  PartitionRingTable,
  SortField,
} from "@/components/ring/partition-ring-table";
import { PartitionStateDistributionChart } from "@/components/ring/partition-state-distribution-chart";
import { PartitionRingFilters } from "@/components/ring/partition-ring-filters";
import { cn } from "@/lib/utils";
import { parseZoneFromOwner, getStateColors } from "@/lib/ring-utils";
import { RefreshLoop } from "@/components/common/refresh-loop";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useToast } from "@/hooks/use-toast";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PartitionInstance } from "@/types/ring";

const STATE_OPTIONS = [
  { value: 1, label: "Pending" },
  { value: 2, label: "Active" },
  { value: 3, label: "Inactive" },
  { value: 4, label: "Deleted" },
] as const;

interface StateChangeMessage {
  type: "success" | "error";
  title: string;
  description: string;
}

interface RateMetrics {
  uncompressedRate: number;
  compressedRate: number;
}

interface NodeMetrics {
  [nodeId: string]: RateMetrics;
}

export default function PartitionRing() {
  const { toast } = useToast();
  const [selectedPartitions, setSelectedPartitions] = useState<Set<number>>(
    new Set()
  );
  const [isForgetLoading, setIsForgetLoading] = useState(false);
  const [forgetProgress, setForgetProgress] = useState<number>(0);
  const [sortField, setSortField] = useState<SortField>("id");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");
  const [idFilter, setIdFilter] = useState<string[]>([]);
  const [stateFilter, setStateFilter] = useState<string[]>([]);
  const [zoneFilter, setZoneFilter] = useState<string[]>([]);
  const [ownerFilter, setOwnerFilter] = useState("");
  const [isForgetDialogOpen, setIsForgetDialogOpen] = useState(false);
  const [isStateChangeLoading, setIsStateChangeLoading] = useState(false);
  const [selectedNewState, setSelectedNewState] = useState<string>();
  const [isStateChangeDialogOpen, setIsStateChangeDialogOpen] = useState(false);
  const [stateChangeMessage, setStateChangeMessage] =
    useState<StateChangeMessage | null>(null);

  // Replace previous partitions state with ref
  const previousMetricsRef = useRef<NodeMetrics>({});

  const {
    partitions,
    error: partitionError,
    isLoading: isPartitionsLoading,
    fetchPartitions,
    changePartitionState,
    uniqueStates,
    uniqueZones,
  } = usePartitionRing({ isPaused: selectedPartitions.size > 0 });

  const { fetchMetrics } = useRateNodeMetrics();
  const [nodeMetrics, setNodeMetrics] = useState<NodeMetrics>({});
  const [isMetricsLoading, setIsMetricsLoading] = useState(false);

  const uniqueNodes = useMemo(
    () =>
      Array.from(
        new Set(
          partitions
            .map((p) => p.owner_id)
            .filter((id): id is string => id !== undefined)
        )
      ),
    [partitions]
  );

  // Denormalize partitions by owner and include rates with trend comparison
  const denormalizedPartitions = useMemo(() => {
    return partitions.flatMap((partition) =>
      partition.owner_ids.map((owner) => {
        const currentMetrics = nodeMetrics[owner] || {
          uncompressedRate: 0,
          compressedRate: 0,
        };
        const previousMetrics = previousMetricsRef.current[owner] || {
          uncompressedRate: 0,
          compressedRate: 0,
        };

        return {
          ...partition,
          owner_id: owner,
          owner_ids: [owner],
          zone: parseZoneFromOwner(owner),
          uncompressedRate: currentMetrics.uncompressedRate,
          compressedRate: currentMetrics.compressedRate,
          previousUncompressedRate: previousMetrics.uncompressedRate,
          previousCompressedRate: previousMetrics.compressedRate,
        };
      })
    );
  }, [partitions, nodeMetrics]);

  // Update previous metrics whenever current metrics change
  useEffect(() => {
    const timeoutId = setTimeout(() => {
      previousMetricsRef.current = nodeMetrics;
    }, 2000); // Add a small delay to ensure we don't update too quickly

    return () => clearTimeout(timeoutId);
  }, [nodeMetrics]);

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
      // Toggle all rows with the same partition ID
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
      // Multi-select partition ID filter
      const matchesId =
        idFilter.length === 0 || idFilter.includes(partition.id.toString());

      // Multi-select state filter
      const matchesState =
        stateFilter.length === 0 ||
        stateFilter.includes(partition.state.toString());

      // Multi-select zone filter
      const matchesZone =
        zoneFilter.length === 0 || zoneFilter.includes(partition.zone);

      // Text search on owner
      const matchesOwner = ownerFilter
        ? partition.owner_id.toLowerCase().includes(ownerFilter.toLowerCase())
        : true;

      return matchesId && matchesState && matchesZone && matchesOwner;
    });
  }, [denormalizedPartitions, idFilter, stateFilter, zoneFilter, ownerFilter]);

  // Memoize selected partition details to prevent re-renders
  const selectedPartitionDetails = useMemo(
    () =>
      denormalizedPartitions.filter((partition) =>
        selectedPartitions.has(partition.id)
      ),
    [denormalizedPartitions, selectedPartitions]
  );

  // Update tableProps to pass both current and previous values
  const tableProps = useMemo(
    () => ({
      partitions: filteredPartitions,
      selectedPartitions,
      onSelectPartition: togglePartition,
      sortField,
      sortDirection,
      onSort: handleSort,
    }),
    [
      filteredPartitions,
      selectedPartitions,
      togglePartition,
      sortField,
      sortDirection,
      handleSort,
    ]
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

      // Show appropriate message
      if (success > 0 && total === success) {
        setStateChangeMessage({
          type: "success",
          title: "State Change Success",
          description: `Successfully changed state for ${success} partition${
            success !== 1 ? "s" : ""
          } to ${STATE_OPTIONS.find((opt) => opt.value === newState)?.label}`,
        });
        await fetchPartitions();
      } else if (success < total) {
        setStateChangeMessage({
          type: "error",
          title: "State Change Failed",
          description: `Failed to change state for ${
            total - success
          } partition${total - success !== 1 ? "s" : ""}.`,
        });
      }

      // Clear selections after completion
      setSelectedPartitions(new Set());
      setSelectedNewState(undefined);
    } catch (err) {
      console.error("Error changing partition states:", err);
      setStateChangeMessage({
        type: "error",
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
  ]);

  const fetchAndTransformMetrics = useCallback(async () => {
    if (uniqueNodes.length === 0) return;

    setIsMetricsLoading(true);
    try {
      const metricsData = await fetchMetrics({
        nodeNames: uniqueNodes,
        metrics: [
          "loki_ingest_storage_reader_fetch_bytes_total",
          "loki_ingest_storage_reader_fetch_compressed_bytes_total",
        ],
      });

      // Transform metrics into the format expected by the table
      const transformedMetrics: NodeMetrics = {};
      Object.entries(metricsData).forEach(([nodeId, rates]) => {
        transformedMetrics[nodeId] = {
          uncompressedRate:
            rates.find(
              (r) => r.name === "loki_ingest_storage_reader_fetch_bytes_total"
            )?.rate || 0,
          compressedRate:
            rates.find(
              (r) =>
                r.name ===
                "loki_ingest_storage_reader_fetch_compressed_bytes_total"
            )?.rate || 0,
        };
      });

      setNodeMetrics(transformedMetrics);
    } catch (error) {
      console.error("Error fetching metrics:", error);
    } finally {
      setIsMetricsLoading(false);
    }
  }, [uniqueNodes, fetchMetrics]);

  // Fetch metrics whenever partitions change
  useEffect(() => {
    fetchAndTransformMetrics();
    const intervalId = setInterval(fetchAndTransformMetrics, 5000);
    return () => clearInterval(intervalId);
  }, [fetchAndTransformMetrics]);

  if (partitionError) {
    return (
      <div className="p-4">
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="text-destructive">Error</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-destructive">{partitionError}</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container space-y-6 p-6">
      {stateChangeMessage && (
        <Alert
          variant={
            stateChangeMessage.type === "error" ? "destructive" : "default"
          }
          className="relative"
        >
          <AlertTitle>{stateChangeMessage.title}</AlertTitle>
          <AlertDescription className="whitespace-pre-line">
            {stateChangeMessage.description}
          </AlertDescription>
          <Button
            variant="ghost"
            size="icon"
            className="absolute top-4 right-4"
            onClick={() => setStateChangeMessage(null)}
          >
            <X className="h-4 w-4" />
          </Button>
        </Alert>
      )}
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
                      {isStateChangeLoading ? (
                        <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                      ) : null}
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
            <PartitionRingTable
              {...tableProps}
              onStateChange={handleStateChange}
            />
          </div>
        </CardContent>
      </Card>

      <Dialog open={isForgetDialogOpen} onOpenChange={setIsForgetDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Forget Partitions</DialogTitle>
            <DialogDescription>
              Are you sure you want to forget the following partitions? This
              action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-[300px] overflow-y-auto">
            <div className="space-y-2">
              {selectedPartitionDetails.map((partition) => (
                <div
                  key={partition.id}
                  className="flex items-center justify-between p-2 rounded-md bg-muted"
                >
                  <div className="flex items-center gap-2">
                    <span className="font-medium">
                      Partition {partition.id}
                    </span>
                    <span
                      className={cn(
                        "inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium",
                        partition.corrupted
                          ? "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200"
                          : "bg-muted"
                      )}
                    >
                      {partition.corrupted ? "Corrupted" : "Healthy"}
                    </span>
                  </div>
                  <span className="text-sm text-muted-foreground">
                    {partition.owner_ids.join(", ")}
                  </span>
                </div>
              ))}
            </div>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setIsForgetDialogOpen(false)}
              disabled={isForgetLoading}
            >
              Cancel
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

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
