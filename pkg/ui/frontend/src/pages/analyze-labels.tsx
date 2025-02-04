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

interface LabelAnalysis {
  name: string;
  uniqueValues: number;
  inStreams: number;
}

type SortField = "name" | "uniqueValues" | "inStreams" | "cardinality";
type MetricType = "uniqueValues" | "inStreams";

export default function AnalyzeLabels() {
  const { cluster } = useCluster();
  const [analysisResults, setAnalysisResults] = useState<{
    totalStreams: number;
    uniqueLabels: number;
    labels: LabelAnalysis[];
  } | null>(null);
  const [sortField, setSortField] = useState<SortField>("uniqueValues");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("desc");
  const [selectedMetric, setSelectedMetric] =
    useState<MetricType>("uniqueValues");

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
      const values = form.getValues();
      const end = new Date();
      const start = new Date(end.getTime() - parseDuration(values.since));

      const response = await fetch(
        `/ui/api/v1/proxy/${nodeName}/loki/api/v1/series?match[]=${encodeURIComponent(
          values.matcher
        )}&start=${start.getTime() * 1e6}&end=${end.getTime() * 1e6}`,
        {
          headers: {
            "X-Scope-OrgID": values.tenant,
          },
        }
      );

      if (!response.ok) {
        throw new Error("Failed to fetch series");
      }

      const data = await response.json();
      const labelMap = new Map<
        string,
        { uniqueValues: Set<string>; inStreams: number }
      >();

      // Process the streams similar to the CLI tool
      data.data.forEach((stream: Record<string, string>) => {
        Object.entries(stream).forEach(([name, value]) => {
          if (!labelMap.has(name)) {
            labelMap.set(name, { uniqueValues: new Set(), inStreams: 0 });
          }
          const label = labelMap.get(name) as {
            uniqueValues: Set<string>;
            inStreams: number;
          };
          label.uniqueValues.add(value);
          label.inStreams++;
        });
      });

      // Convert to array and sort by unique values count
      const labels = Array.from(labelMap.entries()).map(([name, stats]) => ({
        name,
        uniqueValues: stats.uniqueValues.size,
        inStreams: stats.inStreams,
      }));
      labels.sort((a, b) => b.uniqueValues - a.uniqueValues);

      setAnalysisResults({
        totalStreams: data.data.length,
        uniqueLabels: labelMap.size,
        labels,
      });

      return data;
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
              className="grid grid-cols-1 md:grid-cols-4 gap-4 items-end"
            >
              <FormField
                control={form.control}
                name="tenant"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Tenant ID</FormLabel>
                    <FormControl>
                      <Input placeholder="Enter tenant ID..." {...field} />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="since"
                render={({ field }) => (
                  <FormItem>
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
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name="matcher"
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>Matcher</FormLabel>
                    <FormControl>
                      <Input
                        placeholder="Enter matcher... (default: {})"
                        {...field}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <Button
                type="submit"
                disabled={isLoading}
                className="md:self-end"
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
                    content={
                      <ChartTooltipContent
                        className="w-[200px]"
                        labelKey="name"
                      />
                    }
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
                    <TableRow key={label.name}>
                      <TableCell className="font-medium">
                        {label.name}
                      </TableCell>
                      <TableCell>{label.uniqueValues}</TableCell>
                      <TableCell>{label.inStreams}</TableCell>
                      <TableCell>
                        {((label.uniqueValues / label.inStreams) * 100).toFixed(
                          2
                        )}
                        %
                      </TableCell>
                    </TableRow>
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
