import { useParams } from "react-router-dom";
import { Card, CardHeader, CardContent, CardTitle } from "@/components/ui/card";
import { ErrorBoundary } from "@/components/shared/errors/error-boundary";
import { useState } from "react";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { CodeBlock } from "@/components/ui/code-block";
import { format } from "date-fns";
import { useNodeDetails } from "@/hooks/use-node-details";
import { useNodeMetrics } from "@/hooks/use-node-metrics";
import { ServiceStateDistribution } from "@/components/nodes/service-state-distribution";
import { ServiceTable } from "@/components/nodes/service-table";
import { StorageTypeIndicator } from "@/components/nodes/storage-type-indicator";
import { Label } from "@/components/ui/label";
import { LogLevelSelect } from "@/components/nodes/log-level-select";
import { Switch } from "@/components/ui/switch";
import { VersionInformation } from "@/components/nodes/version-information";
import { NodeStatusIndicator } from "@/components/nodes/node-status-indicator";
import { PprofControls } from "@/components/nodes/pprof-controls";
import { CopyButton } from "@/components/common/copy-button";
import { PageContainer } from "@/layout/page-container";
function NodeDetailsPage() {
  const { nodeName } = useParams();
  const [activeTab, setActiveTab] = useState("config");
  const { nodeDetails, isLoading, error } = useNodeDetails(nodeName);
  const {
    metrics: rawMetrics,
    isLoading: isLoadingMetrics,
    error: metricsError,
  } = useNodeMetrics(nodeName, activeTab === "raw-metrics");
  const [showTable, setShowTable] = useState(false);

  if (isLoading) {
    return (
      <div className="container space-y-6 p-6">
        <div className="flex items-center justify-center">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
          <span className="ml-2 text-sm text-muted-foreground">Loading...</span>
        </div>
      </div>
    );
  }

  if (!nodeDetails) {
    return (
      <div className="container space-y-6 p-6">
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
    <PageContainer>
      {/* Main Node Card */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-4">
            <div className="flex-1">
              <CardTitle>
                <h2 className="text-3xl font-semibold tracking-tight">
                  <div className="flex items-center gap-2">
                    {nodeDetails.target} - {nodeName}
                    <CopyButton text={nodeName || ""} />
                  </div>
                </h2>
              </CardTitle>
            </div>
            <NodeStatusIndicator nodeName={nodeName || ""} />
          </div>
        </CardHeader>
        <CardContent className="space-y-6">
          {/* Three Information Cards */}
          <div className="grid grid-cols-3 gap-4">
            <VersionInformation
              build={nodeDetails.build}
              edition={nodeDetails.edition}
              os={nodeDetails.os}
              arch={nodeDetails.arch}
            />

            <Card>
              <CardHeader>
                <CardTitle>Cluster Information</CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-2">
                  <div className="space-y-2">
                    <Label>Cluster ID</Label>
                    <p className="text-sm">{nodeDetails.clusterID}</p>
                  </div>
                  <div className="space-y-2">
                    <Label>Created</Label>
                    <p className="text-sm">
                      {format(nodeDetails.clusterSeededAt, "PPpp")}
                    </p>
                  </div>
                  <div className="space-y-2">
                    <Label>Storage</Label>
                    <p>
                      <StorageTypeIndicator
                        type={(
                          nodeDetails.metrics.store_object_type || "filesystem"
                        ).toLowerCase()}
                        className=""
                      />
                    </p>
                  </div>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between">
                <CardTitle>Service Status</CardTitle>
                <div className="flex items-center space-x-2">
                  <Label htmlFor="view-mode">Table View</Label>
                  <Switch
                    id="view-mode"
                    checked={showTable}
                    onCheckedChange={setShowTable}
                  />
                </div>
              </CardHeader>
              <CardContent>
                {showTable ? (
                  <ServiceTable services={nodeDetails.services} />
                ) : (
                  <ServiceStateDistribution services={nodeDetails.services} />
                )}
              </CardContent>
            </Card>
          </div>

          {/* Controls Section */}
          <div className="flex items-center gap-6">
            <div className="flex items-center gap-2 mr-4">
              <Label>Log Level</Label>
              <LogLevelSelect nodeName={nodeName || ""} />
            </div>

            <PprofControls nodeName={nodeName || ""} />
          </div>

          <div>
            <Tabs defaultValue="config" onValueChange={setActiveTab}>
              <TabsList>
                <TabsTrigger value="config">Configuration</TabsTrigger>
                <TabsTrigger value="metrics">Analytics</TabsTrigger>
                <TabsTrigger value="raw-metrics">Raw Metrics</TabsTrigger>
              </TabsList>
              <TabsContent value="config" className="mt-6">
                <CodeBlock
                  language="yaml"
                  code={nodeDetails.config}
                  fileName="loki.yaml"
                />
              </TabsContent>
              <TabsContent value="metrics" className="mt-6">
                {nodeDetails.metrics && (
                  <CodeBlock
                    code={JSON.stringify(nodeDetails.metrics, null, 2)}
                    language="json"
                    fileName="analytics.json"
                  />
                )}
              </TabsContent>
              <TabsContent value="raw-metrics" className="mt-6">
                {isLoadingMetrics ? (
                  <div className="flex items-center justify-center p-6">
                    <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent"></div>
                    <span className="ml-2 text-sm text-muted-foreground">
                      Loading metrics...
                    </span>
                  </div>
                ) : metricsError ? (
                  <div className="bg-red-50 dark:bg-red-900 border-l-4 border-red-400 p-4">
                    <p className="text-sm text-red-700 dark:text-red-200">
                      {metricsError}
                    </p>
                  </div>
                ) : rawMetrics ? (
                  <CodeBlock
                    code={rawMetrics}
                    language="yaml"
                    fileName="metrics"
                  />
                ) : null}
              </TabsContent>
            </Tabs>
          </div>
        </CardContent>
      </Card>
    </PageContainer>
  );
}

export default function NodeDetailsPageWithErrorBoundary() {
  return (
    <ErrorBoundary>
      <NodeDetailsPage />
    </ErrorBoundary>
  );
}
