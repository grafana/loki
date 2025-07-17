import { useState } from "react";
import { SampledQuery } from "@/types/goldfish";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { CheckCircle2, XCircle, Clock, Database, Zap, FileText, Hash, AlertCircle, ChevronDown } from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { cn } from "@/lib/utils";

interface MetricComparison {
  label: string;
  icon?: React.ReactNode;
  valueA: number | null;
  valueB: number | null;
  formatter: (value: number | null) => string;
  lowerIsBetter: boolean;
  unit?: string;
}

function formatBytes(bytes: number | null): string {
  if (bytes === null) return "N/A";
  if (bytes === 0) return "0 B";
  
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

function formatNumber(num: number | null): string {
  if (num === null) return "N/A";
  return new Intl.NumberFormat().format(num);
}

function formatDuration(ms: number | null): string {
  if (ms === null) return "N/A";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function getComparisonClass(valueA: number | null, valueB: number | null, lowerIsBetter: boolean): {
  classA: string;
  classB: string;
  winner: "A" | "B" | "tie" | null;
} {
  if (valueA === null || valueB === null) {
    return { classA: "", classB: "", winner: null };
  }
  
  const percentDiff = Math.abs((valueB - valueA) / valueA) * 100;
  const threshold = 5; // 5% threshold for significant difference
  
  if (percentDiff < threshold) {
    return { 
      classA: "text-muted-foreground", 
      classB: "text-muted-foreground", 
      winner: "tie" 
    };
  }
  
  if (lowerIsBetter) {
    if (valueA < valueB) {
      return { 
        classA: "text-green-600 font-semibold", 
        classB: "text-red-600", 
        winner: "A" 
      };
    } else {
      return { 
        classA: "text-red-600", 
        classB: "text-green-600 font-semibold", 
        winner: "B" 
      };
    }
  } else {
    if (valueA > valueB) {
      return { 
        classA: "text-green-600 font-semibold", 
        classB: "text-red-600", 
        winner: "A" 
      };
    } else {
      return { 
        classA: "text-red-600", 
        classB: "text-green-600 font-semibold", 
        winner: "B" 
      };
    }
  }
}

function MetricRow({ metric }: { metric: MetricComparison }) {
  const comparison = getComparisonClass(metric.valueA, metric.valueB, metric.lowerIsBetter);
  
  return (
    <div className="grid grid-cols-7 gap-4 py-3 items-center">
      <div className="col-span-2 flex items-center gap-2 text-sm text-muted-foreground">
        {metric.icon}
        <span>{metric.label}</span>
      </div>
      <div className={cn("col-span-2 text-right font-mono text-sm", comparison.classA)}>
        {metric.formatter(metric.valueA)}
      </div>
      <div className="col-span-1 text-center">
        {comparison.winner === "A" && <div className="text-green-600">◀</div>}
        {comparison.winner === "B" && <div className="text-green-600">▶</div>}
        {comparison.winner === "tie" && <div className="text-muted-foreground">≈</div>}
      </div>
      <div className={cn("col-span-2 text-left font-mono text-sm", comparison.classB)}>
        {metric.formatter(metric.valueB)}
      </div>
    </div>
  );
}

export function QueryDiffView({ query }: { query: SampledQuery }) {
  const [isOpen, setIsOpen] = useState(false);
  
  const performanceMetrics: MetricComparison[] = [
    {
      label: "Execution Time",
      icon: <Clock className="h-4 w-4" />,
      valueA: query.cellAExecTimeMs,
      valueB: query.cellBExecTimeMs,
      formatter: formatDuration,
      lowerIsBetter: true,
    },
    {
      label: "Queue Time",
      icon: <Clock className="h-4 w-4" />,
      valueA: query.cellAQueueTimeMs,
      valueB: query.cellBQueueTimeMs,
      formatter: formatDuration,
      lowerIsBetter: true,
    },
    {
      label: "Bytes Processed",
      icon: <Database className="h-4 w-4" />,
      valueA: query.cellABytesProcessed,
      valueB: query.cellBBytesProcessed,
      formatter: formatBytes,
      lowerIsBetter: false, // More bytes processed is generally better (higher throughput)
    },
    {
      label: "Lines Processed",
      icon: <FileText className="h-4 w-4" />,
      valueA: query.cellALinesProcessed,
      valueB: query.cellBLinesProcessed,
      formatter: formatNumber,
      lowerIsBetter: false,
    },
    {
      label: "Bytes/Second",
      icon: <Zap className="h-4 w-4" />,
      valueA: query.cellABytesPerSecond,
      valueB: query.cellBBytesPerSecond,
      formatter: formatBytes,
      lowerIsBetter: false, // Higher throughput is better
    },
    {
      label: "Lines/Second",
      icon: <Zap className="h-4 w-4" />,
      valueA: query.cellALinesPerSecond,
      valueB: query.cellBLinesPerSecond,
      formatter: formatNumber,
      lowerIsBetter: false,
    },
    {
      label: "Entries Returned",
      icon: <FileText className="h-4 w-4" />,
      valueA: query.cellAEntriesReturned,
      valueB: query.cellBEntriesReturned,
      formatter: formatNumber,
      lowerIsBetter: false,
    },
    {
      label: "Splits",
      valueA: query.cellASplits,
      valueB: query.cellBSplits,
      formatter: formatNumber,
      lowerIsBetter: true, // Fewer splits is generally better
    },
    {
      label: "Shards",
      valueA: query.cellAShards,
      valueB: query.cellBShards,
      formatter: formatNumber,
      lowerIsBetter: false,
    },
  ];

  const responseMatch = query.cellAResponseHash === query.cellBResponseHash;
  const statusMatch = query.cellAStatusCode === query.cellBStatusCode;
  const bothSuccessful = 
    query.cellAStatusCode !== null && 
    query.cellBStatusCode !== null &&
    query.cellAStatusCode >= 200 && 
    query.cellAStatusCode < 300 &&
    query.cellBStatusCode >= 200 && 
    query.cellBStatusCode < 300;

  return (
    <Card>
      <Collapsible open={isOpen} onOpenChange={setIsOpen}>
        <CollapsibleTrigger asChild>
          <div className="cursor-pointer hover:bg-muted/50 transition-colors">
            <div className="p-4 space-y-2">
              {/* Query as main header */}
              <div className="flex items-start gap-2">
                <ChevronDown 
                  className={cn(
                    "h-4 w-4 mt-0.5 transition-transform text-muted-foreground",
                    isOpen && "rotate-180"
                  )} 
                />
                <code className="flex-1 text-xs font-mono break-all line-clamp-2">
                  {query.query}
                </code>
              </div>
              
              {/* Key info bar */}
              <div className="flex items-center justify-between pl-6">
                <div className="flex items-center gap-3 text-sm text-muted-foreground">
                  <span>{formatDistanceToNow(new Date(query.sampledAt), { addSuffix: true })}</span>
                  <Badge variant="outline" className="text-xs">{query.tenantId}</Badge>
                  <Badge variant="secondary" className="text-xs">{query.queryType}</Badge>
                </div>
                <div className="flex items-center gap-2">
                  {responseMatch && bothSuccessful ? (
                    <div className="flex items-center gap-1 text-green-600 text-sm">
                      <CheckCircle2 className="h-4 w-4" />
                      <span>Match</span>
                    </div>
                  ) : !bothSuccessful ? (
                    <div className="flex items-center gap-1 text-orange-600 text-sm">
                      <AlertCircle className="h-4 w-4" />
                      <span>Error</span>
                    </div>
                  ) : (
                    <div className="flex items-center gap-1 text-red-600 text-sm">
                      <XCircle className="h-4 w-4" />
                      <span>Mismatch</span>
                    </div>
                  )}
                </div>
              </div>
            </div>
          </div>
        </CollapsibleTrigger>
        
        <CollapsibleContent>
          <Separator />
          <CardContent className="pt-4 space-y-6">
            {/* Response Status */}
            <div className="space-y-3">
              <h4 className="text-sm font-medium">Response Status</h4>
              <div className="grid grid-cols-7 gap-4">
                <div className="col-span-2 text-sm text-muted-foreground">HTTP Status</div>
                <div className="col-span-2 text-right">
                  <Badge variant={query.cellAStatusCode === 200 ? "default" : "destructive"}>
                    {query.cellAStatusCode || "N/A"}
                  </Badge>
                </div>
                <div className="col-span-1 text-center">
                  {statusMatch ? (
                    <CheckCircle2 className="h-4 w-4 text-green-600 mx-auto" />
                  ) : (
                    <AlertCircle className="h-4 w-4 text-orange-600 mx-auto" />
                  )}
                </div>
                <div className="col-span-2 text-left">
                  <Badge variant={query.cellBStatusCode === 200 ? "default" : "destructive"}>
                    {query.cellBStatusCode || "N/A"}
                  </Badge>
                </div>
              </div>
              
              <div className="grid grid-cols-7 gap-4">
                <div className="col-span-2 text-sm text-muted-foreground flex items-center gap-2">
                  <Hash className="h-4 w-4" />
                  Response Hash
                </div>
                <div className="col-span-2 text-right font-mono text-xs text-muted-foreground">
                  {query.cellAResponseHash ? `${query.cellAResponseHash.substring(0, 8)}...` : "N/A"}
                </div>
                <div className="col-span-1 text-center">
                  {responseMatch && bothSuccessful ? (
                    <CheckCircle2 className="h-4 w-4 text-green-600 mx-auto" />
                  ) : !bothSuccessful ? (
                    <AlertCircle className="h-4 w-4 text-orange-600 mx-auto" />
                  ) : (
                    <XCircle className="h-4 w-4 text-red-600 mx-auto" />
                  )}
                </div>
                <div className="col-span-2 text-left font-mono text-xs text-muted-foreground">
                  {query.cellBResponseHash ? `${query.cellBResponseHash.substring(0, 8)}...` : "N/A"}
                </div>
              </div>
            </div>

            <Separator />

            {/* Performance Metrics */}
            <div className="space-y-1">
              <div className="grid grid-cols-7 gap-4 pb-2">
                <div className="col-span-2 text-sm font-medium">Metric</div>
                <div className="col-span-2 text-right text-sm font-medium">Cell A</div>
                <div className="col-span-1"></div>
                <div className="col-span-2 text-left text-sm font-medium">Cell B</div>
              </div>
              {performanceMetrics.map((metric, i) => (
                <MetricRow key={i} metric={metric} />
              ))}
            </div>
          </CardContent>
        </CollapsibleContent>
      </Collapsible>
    </Card>
  );
}