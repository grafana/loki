import { useEffect, useState, useCallback } from "react";
import Layout from "../components/layout/Layout";
import NodeFilters from "../components/nodes/NodeFilters";
import NodeList from "../components/nodes/NodeList";
import { Cluster, Member, NodeState } from "../types";

const NodesPage = () => {
  const [cluster, setCluster] = useState<Cluster | null>(null);
  const [error, setError] = useState<string | null>(null);
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

  const fetchCluster = async () => {
    try {
      const response = await fetch("/ui/api/v1/cluster");
      if (!response.ok) {
        throw new Error(`Failed to fetch cluster data: ${response.statusText}`);
      }
      const data = await response.json();
      setCluster(data);
      setError(null);
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "An unknown error occurred"
      );
    }
  };

  useEffect(() => {
    fetchCluster();
  }, []);

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
      const matchesTarget = !targetFilter || node.target === targetFilter;

      // Show node if any of its services match any of the selected states
      const hasMatchingService =
        selectedStates.length === 0 ||
        node.services.some(
          (service) =>
            service.status &&
            selectedStates.includes(service.status as NodeState)
        );

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
    <Layout>
      <div className="p-6">
        <h1 className="text-3xl font-bold text-gray-900 dark:text-white mb-6">
          Loki Nodes
        </h1>

        {error && (
          <div className="bg-red-50 dark:bg-red-900 border-l-4 border-red-400 p-4 mb-6">
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

        <NodeFilters
          nameFilter={nameFilter}
          targetFilter={targetFilter}
          selectedStates={selectedStates}
          onNameFilterChange={setNameFilter}
          onTargetFilterChange={setTargetFilter}
          onStatesChange={setSelectedStates}
          onRefresh={fetchCluster}
          availableTargets={getAvailableTargets()}
        />

        <NodeList
          nodes={filterNodes()}
          sortField={sortField}
          sortDirection={sortDirection}
          onSort={handleSort}
        />
      </div>
    </Layout>
  );
};

export default NodesPage;
