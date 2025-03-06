import { useState, useMemo, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import * as z from "zod";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useCluster } from "@/contexts/use-cluster";
import { findNodeName } from "@/lib/utils";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ChartConfig,
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
} from "@/components/ui/chart";
import { DataTableColumnHeader } from "@/components/common/data-table-column-header";
import { ChevronDown } from "lucide-react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { useToast } from "@/hooks/use-toast";
import { absolutePath } from "@/util";

const formSchema = z.object({
  tenant: z.string().min(1, "Tenant ID is required"),
  since: z.string(),
  matcher: z.string().default("{}"),
});

const durationOptions = [
  { value: "1h", label: "Last 1 hour" },
  { value: "3h", label: "Last 3 hours" },
  { value: "6h", label: "Last 6 hours" },
  { value: "12h", label: "Last 12 hours" },
  { value: "24h", label: "Last 24 hours" },
];

interface LabelValue {
  value: string;
  count: number;
}

interface LabelAnalysis {
  name: string;
  uniqueValues: number;
  inStreams: number;
  sampleValues: LabelValue[];
}

type SortField = "name" | "uniqueValues" | "inStreams" | "cardinality";
type MetricType = "uniqueValues" | "inStreams";

interface LabelValuesListProps {
  values: LabelValue[];
  totalValues: number;
}

function LabelValuesList({ values, totalValues }: LabelValuesListProps) {
  return (
    <div className="space-y-2 py-2">
      {values.map(({ value, count }) => (
        <div
          key={value}
          className="grid grid-cols-[200px_1fr_80px] items-center gap-4"
        >
          <Badge
            variant="outline"
            className="font-mono text-xs justify-self-start overflow-hidden"
          >
            {value}
          </Badge>
          <div className="h-2 bg-muted rounded-full overflow-hidden">
            <div
              className="h-full bg-primary"
              style={{ width: `${(count / totalValues) * 100}%` }}
            />
          </div>
          <span className="text-xs text-muted-foreground tabular-nums justify-self-end">
            {((count / totalValues) * 100).toFixed(1)}%
          </span>
        </div>
      ))}
    </div>
  );
}

export default function AnalyzeLabels() {
  const { cluster } = useCluster();
  const { toast } = useToast();
  const [analysisResults, setAnalysisResults] = useState<{
    totalStreams: number;
    uniqueLabels: number;
    labels: LabelAnalysis[];
  } | null>(null);
  const [sortField, setSortField] = useState<SortField>("uniqueValues");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("desc");
  const [selectedMetric, setSelectedMetric] =
    useState<MetricType>("uniqueValues");
  const [openRows, setOpenRows] = useState<Set<string>>(new Set());

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      matcher: "{}",
      since: "1h",
    },
  });

  const nodeName = findNodeName(cluster?.members, "query-frontend");

  const { isLoading, refetch } = useQuery({
    queryKey: ["analyze-labels"],
    queryFn: async () => {
      try {
        const values = form.getValues();
        const end = new Date();
        const start = new Date(end.getTime() - parseDuration(values.since));

        const response = await fetch(
          absolutePath(
            `/api/v1/proxy/${nodeName}/loki/api/v1/series?match[]=${encodeURIComponent(
              values.matcher
            )}&start=${start.getTime() * 1e6}&end=${end.getTime() * 1e6}`
          ),
          {
            headers: {
              "X-Scope-OrgID": values.tenant,
            },
          }
        );

        if (!response.ok) {
          const error = await response.text();
          throw new Error(error || "Failed to fetch series");
        }

        const data = await response.json();
        const labelMap = new Map<
          string,
          { uniqueValues: Set<string>; inStreams: number }
        >();
        const valueCountMap = new Map<string, Map<string, number>>();

        // Process the streams similar to the CLI tool
        data.data.forEach((stream: Record<string, string>) => {
          Object.entries(stream).forEach(([name, value]) => {
            if (!labelMap.has(name)) {
              labelMap.set(name, { uniqueValues: new Set(), inStreams: 0 });
              valueCountMap.set(name, new Map());
            }
            const label = labelMap.get(name) as {
              uniqueValues: Set<string>;
              inStreams: number;
            };
            const valueCounts = valueCountMap.get(name)!;

            label.uniqueValues.add(value);
            label.inStreams++;
            valueCounts.set(value, (valueCounts.get(value) || 0) + 1);
          });
        });

        // Convert to array and sort by unique values count
        const labels = Array.from(labelMap.entries()).map(([name, stats]) => {
          const valueCounts = Array.from(valueCountMap.get(name)!.entries())
            .map(([value, count]) => ({ value, count }))
            .sort((a, b) => b.count - a.count)
            .slice(0, 5);

          return {
            name,
            uniqueValues: stats.uniqueValues.size,
            inStreams: stats.inStreams,
            sampleValues: valueCounts,
          };
        });
        labels.sort((a, b) => b.uniqueValues - a.uniqueValues);

        setAnalysisResults({
          totalStreams: data.data.length,
          uniqueLabels: labelMap.size,
          labels,
        });

        return data;
      } catch (error) {
        toast({
          variant: "destructive",
          title: "Error analyzing labels",
          description:
            error instanceof Error
              ? error.message
              : "An unexpected error occurred",
        });
        throw error;
      }
    },
    enabled: false,
  });

  function onSubmit() {
    refetch();
  }

  // Generate CSS variables for colors
  const colorStyles = useMemo(() => {
    const style = document.createElement("style");
    const colors =
      analysisResults?.labels
        .slice(0, 10)
        .map((_, index) => {
          const hue = (index * 137.5) % 360;
          return `--chart-color-${index}: hsl(${hue}, 70%, 50%);`;
        })
        .join("\n") || "";
    style.textContent = `:root { ${colors} }`;
    document.head.appendChild(style);
    return () => style.remove();
  }, [analysisResults]);

  // Use effect to cleanup styles
  useEffect(() => {
    return colorStyles;
  }, [colorStyles]);

  // Update chart configuration
  const chartConfig = {
    value: {
      label:
        selectedMetric === "uniqueValues"
          ? "Unique Values"
          : "Found In Streams",
      theme: {
        light: "var(--chart-color-0)",
        dark: "var(--chart-color-0)",
      },
    },
  } satisfies ChartConfig;

  const sortedLabels = useMemo(() => {
    if (!analysisResults) return [];

    return [...analysisResults.labels].sort((a, b) => {
      let comparison = 0;
      switch (sortField) {
        case "name":
          comparison = a.name.localeCompare(b.name);
          break;
        case "uniqueValues":
          comparison = a.uniqueValues - b.uniqueValues;
          break;
        case "inStreams":
          comparison = a.inStreams - b.inStreams;
          break;
        case "cardinality":
          comparison =
            a.uniqueValues / a.inStreams - b.uniqueValues / b.inStreams;
          break;
      }
      return sortDirection === "asc" ? comparison : -comparison;
    });
  }, [analysisResults, sortField, sortDirection]);

  return (
    <div className="container mx-auto p-4 space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Analyze Labels</CardTitle>
          <CardDescription>
            Analyze label distribution across your log streams
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Form {...form}>
            <form
              onSubmit={form.handleSubmit(onSubmit)}
              className="grid grid-cols-1 md:grid-cols-4 gap-4"
            >
              <FormField
                control={form.control}
                name="tenant"
                render={({ field }) => (
                  <FormItem className="flex flex-col space-y-1.5">
                    <FormLabel>Tenant ID</FormLabel>
                    <FormControl>
                      <Input placeholder="Enter tenant ID..." {...field} />
                    </FormControl>
                    <FormMessage className="text-xs" />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="since"
                render={({ field }) => (
                  <FormItem className="flex flex-col space-y-1.5">
                    <FormLabel>Time Range</FormLabel>
                    <Select
                      onValueChange={field.onChange}
                      defaultValue={field.value}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue placeholder="Select time range" />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent>
                        {durationOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FormMessage className="text-xs" />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="matcher"
                render={({ field }) => (
                  <FormItem className="flex flex-col space-y-1.5">
                    <FormLabel>Matcher</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="Enter matcher... (default: {})"
                        {...field}
                      />
                    </FormControl>
                    <FormMessage className="text-xs" />
                  </FormItem>
                )}
              />

              <Button
                type="submit"
                disabled={isLoading}
                className="self-end h-10"
              >
                {isLoading ? "Analyzing..." : "Analyze"}
              </Button>
            </form>
          </Form>
        </CardContent>
      </Card>

      {analysisResults && (
        <>
          <Card>
            <CardHeader className="flex flex-col items-stretch space-y-0 border-b p-0 sm:flex-row">
              <div className="flex flex-1 flex-col justify-center gap-1 px-6 py-5 sm:py-6">
                <CardTitle>Label Distribution</CardTitle>
                <CardDescription>
                  Top 20 labels by unique values
                </CardDescription>
              </div>
              <div className="flex">
                <div className="relative z-30 flex flex-1 flex-col justify-center gap-1 px-6 py-4 text-left sm:px-8 sm:py-6">
                  <span className="text-xs text-muted-foreground">
                    Total Streams
                  </span>
                  <span className="text-lg font-bold leading-none sm:text-3xl">
                    {analysisResults.totalStreams.toLocaleString()}
                  </span>
                </div>
                <div className="relative z-30 flex flex-1 flex-col justify-center gap-1 border-l px-6 py-4 text-left sm:px-8 sm:py-6">
                  <span className="text-xs text-muted-foreground">
                    Unique Labels
                  </span>
                  <span className="text-lg font-bold leading-none sm:text-3xl">
                    {analysisResults.uniqueLabels.toLocaleString()}
                  </span>
                </div>
              </div>
            </CardHeader>
            <CardContent className="px-2 sm:p-6">
              <div className="mb-4">
                <Select
                  value={selectedMetric}
                  onValueChange={(value: MetricType) =>
                    setSelectedMetric(value)
                  }
                >
                  <SelectTrigger className="w-[200px]">
                    <SelectValue placeholder="Select metric" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="uniqueValues">Unique Values</SelectItem>
                    <SelectItem value="inStreams">Found In Streams</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <ChartContainer
                config={chartConfig}
                className="aspect-auto h-[500px] w-full"
              >
                <BarChart
                  data={analysisResults.labels
                    .slice(0, 20)
                    .map((label, index) => ({
                      name: label.name,
                      value:
                        selectedMetric === "uniqueValues"
                          ? label.uniqueValues
                          : label.inStreams,
                      fill: `var(--chart-color-${index})`,
                    }))}
                  layout="vertical"
                  margin={{
                    right: 24,
                  }}
                  barSize={
                    ((500 - 48) / Math.min(20, analysisResults.labels.length)) *
                    0.6
                  }
                  maxBarSize={24}
                >
                  <CartesianGrid horizontal={false} />
                  <YAxis
                    dataKey="name"
                    type="category"
                    tickLine={false}
                    axisLine={false}
                    width={90}
                    fontSize={11}
                    interval={0}
                  />
                  <XAxis
                    type="number"
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                  />
                  <ChartTooltip
                    content={<ChartTooltipContent className="w-[200px]" />}
                  />
                  <Bar
                    dataKey="value"
                    fillOpacity={0.8}
                    radius={[4, 4, 0, 0]}
                  />
                </BarChart>
              </ChartContainer>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Label Details</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      <DataTableColumnHeader
                        title="Label Name"
                        field="name"
                        sortField={sortField}
                        sortDirection={sortDirection}
                        onSort={(field) => {
                          if (field === sortField) {
                            setSortDirection(
                              sortDirection === "asc" ? "desc" : "asc"
                            );
                          } else {
                            setSortField(field as SortField);
                            setSortDirection("desc");
                          }
                        }}
                      />
                    </TableHead>
                    <TableHead>
                      <DataTableColumnHeader
                        title="Unique Values"
                        field="uniqueValues"
                        sortField={sortField}
                        sortDirection={sortDirection}
                        onSort={(field) => {
                          if (field === sortField) {
                            setSortDirection(
                              sortDirection === "asc" ? "desc" : "asc"
                            );
                          } else {
                            setSortField(field as SortField);
                            setSortDirection("desc");
                          }
                        }}
                      />
                    </TableHead>
                    <TableHead>
                      <DataTableColumnHeader
                        title="Found In Streams"
                        field="inStreams"
                        sortField={sortField}
                        sortDirection={sortDirection}
                        onSort={(field) => {
                          if (field === sortField) {
                            setSortDirection(
                              sortDirection === "asc" ? "desc" : "asc"
                            );
                          } else {
                            setSortField(field as SortField);
                            setSortDirection("desc");
                          }
                        }}
                      />
                    </TableHead>
                    <TableHead>
                      <DataTableColumnHeader
                        title="Cardinality %"
                        field="cardinality"
                        sortField={sortField}
                        sortDirection={sortDirection}
                        onSort={(field) => {
                          if (field === sortField) {
                            setSortDirection(
                              sortDirection === "asc" ? "desc" : "asc"
                            );
                          } else {
                            setSortField(field as SortField);
                            setSortDirection("desc");
                          }
                        }}
                      />
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {sortedLabels.map((label) => (
                    <Collapsible
                      key={label.name}
                      asChild
                      open={openRows.has(label.name)}
                      onOpenChange={(isOpen) => {
                        const newOpenRows = new Set(openRows);
                        if (isOpen) {
                          newOpenRows.add(label.name);
                        } else {
                          newOpenRows.delete(label.name);
                        }
                        setOpenRows(newOpenRows);
                      }}
                    >
                      <>
                        <TableRow>
                          <TableCell className="font-medium">
                            <CollapsibleTrigger className="flex items-center gap-2 hover:text-primary">
                              <ChevronDown
                                className={cn(
                                  "h-4 w-4 transition-transform",
                                  openRows.has(label.name) && "rotate-180"
                                )}
                              />
                              {label.name}
                            </CollapsibleTrigger>
                          </TableCell>
                          <TableCell>
                            {label.uniqueValues.toLocaleString()}
                          </TableCell>
                          <TableCell>
                            {label.inStreams.toLocaleString()}
                          </TableCell>
                          <TableCell>
                            {(
                              (label.uniqueValues / label.inStreams) *
                              100
                            ).toFixed(2)}
                            %
                          </TableCell>
                        </TableRow>
                        <CollapsibleContent asChild>
                          <TableRow>
                            <TableCell
                              colSpan={4}
                              className="border-t-0 bg-muted/5"
                            >
                              <div className="px-4">
                                <LabelValuesList
                                  values={label.sampleValues}
                                  totalValues={label.inStreams}
                                />
                              </div>
                            </TableCell>
                          </TableRow>
                        </CollapsibleContent>
                      </>
                    </Collapsible>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

function parseDuration(duration: string): number {
  const value = parseInt(duration);
  const unit = duration.slice(-1);
  const multiplier = unit === "h" ? 3600000 : 0; // Convert hours to milliseconds
  return value * multiplier;
}
