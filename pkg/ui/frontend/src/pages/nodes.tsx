import { useState } from "react";
import NodeFilters from "@/components/nodes/node-filters";
import NodeList from "@/components/nodes/node-list";
import { TargetDistributionChart } from "@/components/nodes/target-distribution-chart";
import { Member, NodeState } from "../types/cluster";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import { ErrorBoundary } from "@/components/shared/errors/error-boundary";
import { useCluster } from "@/contexts/use-cluster";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { AlertCircle } from "lucide-react";
import { PageContainer } from "@/layout/page-container";

const NodesPage = () => {
  const { cluster, error, refresh, isLoading } = useCluster();
  const [nameFilter, setNameFilter] = useState("");
  const [targetFilter, setTargetFilter] = useState<string[]>([]);
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

  const handleSort = (field: "name" | "target" | "version" | "buildDate") => {
    if (field === sortField) {
      setSortDirection(sortDirection === "asc" ? "desc" : "asc");
    } else {
      setSortField(field);
      setSortDirection("asc");
    }
  };

  const filterNodes = () => {
    if (!cluster) return {};

    return Object.entries(cluster.members).reduce((acc, [name, node]) => {
      const matchesName = name.toLowerCase().includes(nameFilter.toLowerCase());
      const matchesTarget =
        !targetFilter ||
        targetFilter.length === 0 ||
        targetFilter.includes(node.target);

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
  };

  const getAvailableTargets = () => {
    if (!cluster) return [];
    const targets = new Set<string>();
    Object.values(cluster.members).forEach((node) => {
      if (node.target) targets.add(node.target);
    });
    return Array.from(targets).sort();
  };

  return (
    <PageContainer>
      <Card className="shadow-sm">
        <CardHeader>
          <div className="grid grid-cols-[1fr_auto] gap-8">
            <div className="space-y-6">
              <div>
                <h2 className="text-3xl font-semibold tracking-tight">Nodes</h2>
                <p className="text-sm text-muted-foreground mt-1">
                  View and manage Loki nodes in your cluster with their current
                  status and configuration
                </p>
              </div>
              <NodeFilters
                nameFilter={nameFilter}
                targetFilter={targetFilter}
                selectedStates={selectedStates}
                onNameFilterChange={setNameFilter}
                onTargetFilterChange={setTargetFilter}
                onStatesChange={setSelectedStates}
                onRefresh={refresh}
                availableTargets={getAvailableTargets()}
                isLoading={isLoading}
              />
            </div>
            <div className="flex items-center">
              <div className="w-[250px]">
                <TargetDistributionChart nodes={filterNodes()} />
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {error && (
              <Alert variant="destructive">
                <AlertCircle className="h-4 w-4" />
                <AlertTitle>Error</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            {isLoading && (
              <div className="flex items-center justify-center py-4">
                <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
                <span className="ml-2 text-sm text-muted-foreground">
                  Loading...
                </span>
              </div>
            )}

            {!isLoading && !error && (
              <NodeList
                nodes={filterNodes()}
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={handleSort}
              />
            )}
          </div>
        </CardContent>
      </Card>
    </PageContainer>
  );
};

export default function NodesPageWithErrorBoundary() {
  return (
    <ErrorBoundary>
      <NodesPage />
    </ErrorBoundary>
  );
}
