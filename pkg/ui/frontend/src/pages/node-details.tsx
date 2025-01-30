import { useParams } from "react-router-dom";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import { ErrorBoundary } from "@/components/error-boundary";
import { useCluster } from "@/hooks/use-cluster";
import { useEffect } from "react";

function NodeDetailsPage() {
  const { nodeName } = useParams();
  const { cluster, error, isLoading, fetchCluster } = useCluster();

  useEffect(() => {
    fetchCluster();
  }, [fetchCluster]);

  const node = cluster?.members[nodeName ?? ""];
  console.log(nodeName);
  console.log(cluster);

  if (isLoading) {
    return (
      <div className="flex items-center justify-center p-6">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
        <span className="ml-2 text-sm text-muted-foreground">Loading...</span>
      </div>
    );
  }

  if (error || !node) {
    return (
      <div className="p-6">
        <div className="bg-red-50 dark:bg-red-900 border-l-4 border-red-400 p-4">
          <div className="flex">
            <div className="ml-3">
              <p className="text-sm text-red-700 dark:text-red-200">
                {error || `Node "${nodeName}" not found`}
              </p>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="p-6">
      <Card className="shadow-sm">
        <CardHeader>
          <div className="space-y-2">
            <h2 className="text-3xl font-semibold tracking-tight">
              {nodeName}
            </h2>
            <p className="text-sm text-muted-foreground">
              Detailed information about this Loki node
            </p>
          </div>
        </CardHeader>
        <CardContent>
          {/* Content will be added in the next iteration */}
        </CardContent>
      </Card>
    </div>
  );
}

export default function NodeDetailsPageWithErrorBoundary() {
  return (
    <ErrorBoundary>
      <NodeDetailsPage />
    </ErrorBoundary>
  );
}
