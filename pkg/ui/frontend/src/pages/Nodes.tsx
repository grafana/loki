import { useEffect, useState, useCallback } from "react";
import NodeFilters from "@/components/nodes/node-filters";
import NodeList from "@/components/nodes/node-list";
import { Member, NodeState, ALL_VALUES_TARGET } from "../types/cluster";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import { ErrorBoundary } from "@/components/error-boundary";
import { useCluster } from "@/hooks/use-cluster";

const NodesPage = () => {
  const { cluster, error, fetchCluster, isLoading } = useCluster();
  const [nameFilter, setNameFilter] = useState("");
  const [targetFilter, setTargetFilter] = useState("");
  const [selectedStates, setSelectedStates] = useState<NodeState[]>([
    "New",
    "Starting",
    "Running",
    "Stopping",
    "Terminated",
    "Failed",
  ]);
  const [sortField, setSortField] = useState<
    "name" | "target" | "version" | "buildDate"
  >("name");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");

  useEffect(() => {
    fetchCluster();
  }, [fetchCluster]);

  const handleSort = (field: "name" | "target" | "version" | "buildDate") => {
    if (field === sortField) {
      setSortDirection(sortDirection === "asc" ? "desc" : "asc");
    } else {
      setSortField(field);
      setSortDirection("asc");
    }
  };

  const filterNodes = useCallback(() => {
    if (!cluster) return {};

    return Object.entries(cluster.members).reduce((acc, [name, node]) => {
      const matchesName = name.toLowerCase().includes(nameFilter.toLowerCase());
      const matchesTarget =
        !targetFilter ||
        targetFilter === ALL_VALUES_TARGET ||
        node.target === targetFilter;

      // Show node if any of its services match any of the selected states
      const hasMatchingService =
        selectedStates.length === 0 ||
        (node.services &&
          Array.isArray(node.services) &&
          node.services.some(
            (service) =>
              service?.status &&
              selectedStates.includes(service.status as NodeState)
          ));

      if (matchesName && matchesTarget && hasMatchingService) {
        acc[name] = node;
      }
      return acc;
    }, {} as { [key: string]: Member });
  }, [cluster, nameFilter, targetFilter, selectedStates]);

  const getAvailableTargets = useCallback(() => {
    if (!cluster) return [];
    const targets = new Set<string>();
    Object.values(cluster.members).forEach((node) => {
      if (node.target) targets.add(node.target);
    });
    return Array.from(targets).sort();
  }, [cluster]);

  return (
    <div className="p-6">
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <h2 className="text-2xl font-semibold">Nodes</h2>
          </div>
          <p className="text-muted-foreground">
            This page shows nodes and their current status
          </p>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <NodeFilters
              nameFilter={nameFilter}
              targetFilter={targetFilter}
              selectedStates={selectedStates}
              onNameFilterChange={setNameFilter}
              onTargetFilterChange={setTargetFilter}
              onStatesChange={setSelectedStates}
              onRefresh={fetchCluster}
              availableTargets={getAvailableTargets()}
              isLoading={isLoading}
            />

            {error && (
              <div className="bg-red-50 dark:bg-red-900 border-l-4 border-red-400 p-4">
                <div className="flex">
                  <div className="flex-shrink-0">
                    <svg
                      className="h-5 w-5 text-red-400"
                      viewBox="0 0 20 20"
                      fill="currentColor"
                    >
                      <path
                        fillRule="evenodd"
                        d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z"
                        clipRule="evenodd"
                      />
                    </svg>
                  </div>
                  <div className="ml-3">
                    <p className="text-sm text-red-700 dark:text-red-200">
                      {error}
                    </p>
                  </div>
                </div>
              </div>
            )}

            {isLoading && (
              <div className="flex items-center justify-center py-4">
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
                <span className="ml-2 text-sm text-muted-foreground">
                  Loading...
                </span>
              </div>
            )}

            <NodeList
              nodes={filterNodes()}
              sortField={sortField}
              sortDirection={sortDirection}
              onSort={handleSort}
            />
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default function NodesPageWithErrorBoundary() {
  return (
    <ErrorBoundary>
      <NodesPage />
    </ErrorBoundary>
  );
}
