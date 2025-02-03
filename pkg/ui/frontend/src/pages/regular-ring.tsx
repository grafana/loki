import { useState, useCallback, useMemo } from "react";
import { RingType } from "@/types/ring";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Loader2 } from "lucide-react";
import { AVAILABLE_RINGS, useRing } from "@/hooks/use-ring";
import {
  RingInstanceTable,
  SortField,
} from "@/components/ring/ring-instance-table";
import { RingFilters } from "@/components/ring/ring-filters";
import { cn } from "@/lib/utils";
import { getStateColors } from "@/lib/ring-utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { RingStateDistributionChart } from "@/components/ring/ring-state-distribution-chart";
import { RefreshLoop } from "@/components/common/refresh-loop";
import { BaseRing } from "./base-ring";
import { useToast } from "@/hooks/use-toast";

interface RegularRingProps {
  ringName: RingType;
}

export function RegularRing({ ringName }: RegularRingProps) {
  const [selectedInstances, setSelectedInstances] = useState<Set<string>>(
    new Set()
  );
  const [isForgetLoading, setIsForgetLoading] = useState(false);
  const [forgetProgress, setForgetProgress] = useState<number>(0);
  const [sortField, setSortField] = useState<SortField>("id");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");
  const [idFilter, setIdFilter] = useState("");
  const [stateFilter, setStateFilter] = useState<string[]>([]);
  const [zoneFilter, setZoneFilter] = useState<string[]>([]);
  const [isForgetDialogOpen, setIsForgetDialogOpen] = useState(false);

  const {
    ring,
    error,
    isLoading,
    fetchRing,
    forgetInstances,
    uniqueStates,
    uniqueZones,
    isTokenBased,
  } = useRing({ ringName, isPaused: selectedInstances.size > 0 });

  // Get selected instance details
  const selectedInstanceDetails = useMemo(() => {
    if (!ring?.shards) return [];
    return ring.shards.filter((instance) => selectedInstances.has(instance.id));
  }, [ring?.shards, selectedInstances]);

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

  // Handle instance selection
  const toggleInstance = useCallback((instanceId: string) => {
    setSelectedInstances((prev) => {
      const newSet = new Set(prev);
      if (newSet.has(instanceId)) {
        newSet.delete(instanceId);
      } else {
        newSet.add(instanceId);
      }
      return newSet;
    });
  }, []);

  const { toast } = useToast();

  // Handle forget instances
  const handleForget = useCallback(async () => {
    if (selectedInstances.size === 0) return;

    try {
      setIsForgetLoading(true);
      setForgetProgress(0);

      const { success, total } = await forgetInstances(
        Array.from(selectedInstances)
      );
      if (success > 0) {
        await fetchRing();
        setSelectedInstances(new Set());
      }

      if (success < total) {
        toast({
          title: "Failed to forget instances",
          description: `Failed to forget ${total - success} instance(s)`,
          variant: "destructive",
        });
      }
    } catch (err) {
      toast({
        title: "Failed to forget instances",
        description: `${error}`,
        variant: "destructive",
      });
    } finally {
      setIsForgetLoading(false);
      setIsForgetDialogOpen(false);
    }
  }, [selectedInstances, forgetInstances, fetchRing, toast, error]);

  // Filter and sort instances
  const sortedInstances = useMemo(() => {
    if (!ring?.shards) return [];

    return ring.shards
      .filter((instance) => {
        const matchesId = instance.id
          .toLowerCase()
          .includes(idFilter.toLowerCase());
        const matchesState =
          stateFilter.length === 0 || stateFilter.includes(instance.state);
        const matchesZone =
          zoneFilter.length === 0 || zoneFilter.includes(instance.zone);
        return matchesId && matchesState && matchesZone;
      })
      .sort((a, b) => {
        let comparison = 0;
        switch (sortField) {
          case "id":
            comparison = a.id.localeCompare(b.id);
            break;
          case "state":
            comparison = a.state.localeCompare(b.state);
            break;
          case "address":
            comparison = a.address.localeCompare(b.address);
            break;
          case "zone":
            comparison = (a.zone || "").localeCompare(b.zone || "");
            break;
          case "timestamp":
            comparison =
              new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime();
            break;
          case "tokens":
            comparison = a.tokens.length - b.tokens.length;
            break;
          case "ownership":
            comparison = parseFloat(a.ownership) - parseFloat(b.ownership);
            break;
        }
        return sortDirection === "asc" ? comparison : -comparison;
      });
  }, [
    ring?.shards,
    idFilter,
    stateFilter,
    zoneFilter,
    sortField,
    sortDirection,
  ]);

  if (error) {
    return <BaseRing error={error} ringName={ringName} />;
  }

  return (
    <div className="container space-y-6 p-6">
      <Card>
        <CardHeader>
          <div className="grid grid-cols-[1fr_auto] gap-8">
            <div className="space-y-6">
              <div>
                <CardTitle className="text-3xl font-semibold tracking-tight">
                  {AVAILABLE_RINGS.find((r) => r.id === ringName)?.title || ""}{" "}
                  Ring Members
                </CardTitle>
                <p className="text-sm text-muted-foreground mt-1">
                  View and manage ring instances with their current status and
                  configuration
                </p>
              </div>
              <div className="flex items-center justify-between min-h-[32px]">
                <RefreshLoop
                  onRefresh={fetchRing}
                  isPaused={selectedInstances.size > 0}
                  isLoading={isLoading}
                />
                {selectedInstances.size > 0 && (
                  <div className="flex items-center gap-4">
                    <span className="text-sm text-muted-foreground">
                      {selectedInstances.size} instance
                      {selectedInstances.size !== 1 ? "s" : ""} selected
                    </span>
                    <Button
                      onClick={() => setIsForgetDialogOpen(true)}
                      disabled={isForgetLoading}
                      size="sm"
                      variant="outline"
                      className={cn(
                        "border-red-200 bg-red-50 text-red-900 hover:bg-red-100 hover:text-red-900",
                        "dark:border-red-800 dark:bg-red-950 dark:text-red-200 dark:hover:bg-red-900",
                        "disabled:hover:bg-red-50 dark:disabled:hover:bg-red-950"
                      )}
                    >
                      {isForgetLoading && (
                        <>
                          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                          {forgetProgress > 0 && (
                            <span className="mr-2">
                              {forgetProgress}/{selectedInstances.size}
                            </span>
                          )}
                        </>
                      )}
                      Forget Selected
                    </Button>
                  </div>
                )}
              </div>
            </div>
            <div className="flex items-center">
              <div className="w-[250px]">
                {ring?.shards && (
                  <RingStateDistributionChart instances={ring.shards} />
                )}
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          <RingFilters
            idFilter={idFilter}
            onIdFilterChange={setIdFilter}
            stateFilter={stateFilter}
            onStateFilterChange={setStateFilter}
            zoneFilter={zoneFilter}
            onZoneFilterChange={setZoneFilter}
            uniqueStates={uniqueStates}
            uniqueZones={uniqueZones}
          />
          <div className="rounded-md border bg-card">
            <RingInstanceTable
              instances={sortedInstances}
              selectedInstances={selectedInstances}
              onSelectInstance={toggleInstance}
              sortField={sortField}
              sortDirection={sortDirection}
              onSort={handleSort}
              showTokens={isTokenBased}
            />
          </div>
        </CardContent>
      </Card>

      <Dialog open={isForgetDialogOpen} onOpenChange={setIsForgetDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Confirm Forget Instances</DialogTitle>
            <DialogDescription>
              Are you sure you want to forget the following instances? This
              action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-[300px] overflow-y-auto">
            <div className="space-y-2">
              {selectedInstanceDetails.map((instance) => (
                <div
                  key={instance.id}
                  className="flex items-center justify-between p-2 rounded-md bg-muted"
                >
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{instance.id}</span>
                    <span
                      className={cn(
                        "inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium",
                        getStateColors(instance.state)
                      )}
                    >
                      {instance.state}
                    </span>
                  </div>
                  <span className="text-sm text-muted-foreground">
                    {instance.address}
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
            <Button
              variant="outline"
              onClick={handleForget}
              disabled={isForgetLoading}
              className={cn(
                "border-red-200 bg-red-50 text-red-900 hover:bg-red-100 hover:text-red-900",
                "dark:border-red-800 dark:bg-red-950 dark:text-red-200 dark:hover:bg-red-900",
                "disabled:hover:bg-red-50 dark:disabled:hover:bg-red-950"
              )}
            >
              {isForgetLoading ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Forgetting...
                </>
              ) : (
                "Forget Instances"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
