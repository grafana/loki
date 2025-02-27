import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useCluster } from "@/contexts/use-cluster";
import { ServiceNames } from "@/lib/ring-utils";
import { findNodeName } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Loader2, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { fromUnixTime, formatDistance, format } from "date-fns";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { DataTableColumnHeader } from "@/components/common/data-table-column-header";
import { Badge } from "@/components/ui/badge";
import { DateHover } from "@/components/common/date-hover";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { Input } from "@/components/ui/input";
import { PageContainer } from "@/layout/page-container";
import { absolutePath } from "@/util";

interface DeleteRequest {
  request_id: string;
  start_time: number;
  end_time: number;
  query: string;
  status: string;
  created_at: number;
  user_id: string;
  deleted_lines: number;
}

const DeleteRequestStatus = {
  Received: "received",
  Processing: "processed",
} as const;

const useDeletes = (status: string[]) => {
  const { cluster } = useCluster();
  const nodeName = useMemo(() => {
    return findNodeName(cluster?.members, ServiceNames.compactor);
  }, [cluster?.members]);

  const { data, isLoading, error } = useQuery<DeleteRequest[]>({
    queryKey: ["deletes", status, nodeName],
    queryFn: async () => {
      try {
        const requests = await Promise.all(
          status.map(async (s) => {
            const response = await fetch(
              absolutePath(
                `/api/v1/proxy/${nodeName}/compactor/ui/api/v1/deletes?status=${s}`
              )
            );
            if (!response.ok) {
              const errorText = await response.text();
              throw new Error(
                errorText || `HTTP error! status: ${response.status}`
              );
            }
            return response.json();
          })
        );
        // Flatten the array of arrays into a single array of delete requests
        return requests.flat();
      } catch (err) {
        throw err instanceof Error
          ? err
          : new Error("Failed to fetch delete requests");
      }
    },
    enabled: !!nodeName,
  });

  return { data, isLoading, error };
};

interface FiltersProps {
  selectedStatus: string[];
  onStatusChange: (status: string[]) => void;
  queryFilter: string;
  onQueryFilterChange: (query: string) => void;
}

const Filters: React.FC<FiltersProps> = ({
  selectedStatus,
  onStatusChange,
  queryFilter,
  onQueryFilterChange,
}) => {
  return (
    <div className="flex items-center gap-4">
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium">Status</span>
        <ToggleGroup
          type="multiple"
          value={selectedStatus}
          onValueChange={(value) => {
            // Ensure at least one status is always selected
            if (value.length > 0) {
              onStatusChange(value);
            }
          }}
          className="justify-start"
        >
          {Object.entries(DeleteRequestStatus).map(([key, value]) => (
            <ToggleGroupItem
              key={value}
              value={value}
              aria-label={`Toggle ${key.toLowerCase()} status`}
              className="capitalize"
            >
              {key}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
      <Input
        type="search"
        placeholder="Filter by query..."
        value={queryFilter}
        onChange={(e) => onQueryFilterChange(e.target.value)}
        className="w-[300px]"
      />
    </div>
  );
};

type DeleteSortField = "status" | "user" | "createdAt" | "duration";

interface DeleteListProps {
  requests: DeleteRequest[];
  sortField: DeleteSortField;
  sortDirection: "asc" | "desc";
  onSort: (field: DeleteSortField) => void;
}

const StatusBadge = ({ status }: { status: string }) => {
  const variant =
    status === DeleteRequestStatus.Received ? "secondary" : "default";
  return (
    <Badge variant={variant} className="capitalize">
      {status}
    </Badge>
  );
};

const RangeHover = ({ start, end }: { start: number; end: number }) => {
  const duration = formatDistance(
    fromUnixTime(start / 1000),
    fromUnixTime(end / 1000)
  );

  const formatUTC = (timestamp: number) => {
    const date = new Date(timestamp);
    return format(
      new Date(date.getTime() + date.getTimezoneOffset() * 60000),
      "yyyy-MM-dd HH:mm:ss"
    );
  };

  return (
    <HoverCard>
      <HoverCardTrigger>
        <span className="cursor-default">{duration}</span>
      </HoverCardTrigger>
      <HoverCardContent className="w-fit">
        <div className="space-y-2">
          <div className="flex flex-col gap-1">
            <div className="flex items-center gap-2">
              <span className="px-2 py-0.5 text-xs font-medium bg-gray-100 rounded dark:bg-gray-700 w-14 text-center">
                From
              </span>
              <span className="font-mono">{formatUTC(start)}</span>
            </div>
            <div className="flex items-center gap-2">
              <span className="px-2 py-0.5 text-xs font-medium bg-gray-100 rounded dark:bg-gray-700 w-14 text-center">
                To
              </span>
              <span className="font-mono">{formatUTC(end)}</span>
            </div>
          </div>
        </div>
      </HoverCardContent>
    </HoverCard>
  );
};

const DeleteList: React.FC<DeleteListProps> = ({
  requests,
  sortField,
  sortDirection,
  onSort,
}) => {
  const sortedRequests = [...requests].sort((a, b) => {
    let comparison = 0;
    let durationA: number;
    let durationB: number;

    switch (sortField) {
      case "status":
        comparison = a.status.localeCompare(b.status);
        break;
      case "user":
        comparison = a.user_id.localeCompare(b.user_id);
        break;
      case "createdAt":
        comparison = a.created_at - b.created_at;
        break;
      case "duration":
        durationA = a.end_time - a.start_time;
        durationB = b.end_time - b.start_time;
        comparison = durationA - durationB;
        break;
    }
    return sortDirection === "asc" ? comparison : -comparison;
  });

  return (
    <div className="rounded-md border bg-card">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="w-[80px]">
              <DataTableColumnHeader<DeleteSortField>
                title="Status"
                field="status"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[100px]">
              <DataTableColumnHeader<DeleteSortField>
                title="User"
                field="user"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<DeleteSortField>
                title="Created At"
                field="createdAt"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[150px]">
              <DataTableColumnHeader<DeleteSortField>
                title="Range"
                field="duration"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[100px]">Deleted Lines</TableHead>
            <TableHead>Query</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sortedRequests.map((request) => (
            <TableRow
              key={`${request.request_id}-${request.start_time}-${request.end_time}`}
            >
              <TableCell className="px-4">
                <StatusBadge status={request.status} />
              </TableCell>
              <TableCell>{request.user_id}</TableCell>
              <TableCell>
                <DateHover date={new Date(request.created_at)} />
              </TableCell>
              <TableCell>
                <RangeHover start={request.start_time} end={request.end_time} />
              </TableCell>
              <TableCell>{request.deleted_lines}</TableCell>
              <TableCell>
                <code className="font-mono text-sm whitespace-pre-wrap break-all">
                  {request.query}
                </code>
              </TableCell>
            </TableRow>
          ))}
          {sortedRequests.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="h-24 text-center">
                <div className="text-muted-foreground">
                  No delete requests found
                </div>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
};

const DeletesPage = () => {
  const [status, setStatus] = useState<string[]>([
    DeleteRequestStatus.Received,
    DeleteRequestStatus.Processing,
  ]);
  const [queryFilter, setQueryFilter] = useState("");
  const [sortField, setSortField] = useState<DeleteSortField>("createdAt");
  const [sortDirection, setSortDirection] = useState<"asc" | "desc">("desc");
  const { data, isLoading, error } = useDeletes(status);

  const filteredData = useMemo(() => {
    if (!data || !queryFilter) return data;
    return data.filter((request) =>
      request.query.toLowerCase().includes(queryFilter.toLowerCase())
    );
  }, [data, queryFilter]);

  const handleSort = (field: DeleteSortField) => {
    if (field === sortField) {
      setSortDirection(sortDirection === "asc" ? "desc" : "asc");
    } else {
      setSortField(field);
      setSortDirection("desc");
    }
  };

  return (
    <PageContainer>
      <Card className="shadow-sm">
        <CardHeader>
          <div className="flex flex-col gap-6">
            <div className="flex items-start justify-between">
              <div>
                <h2 className="text-3xl font-semibold tracking-tight">
                  Delete Requests
                </h2>
                <p className="text-sm text-muted-foreground mt-1">
                  View and manage delete requests in your cluster
                </p>
              </div>
              <Button variant="default" asChild>
                <Link to="/tenants/deletes/new">
                  <Plus className="mr-2 h-4 w-4" />
                  New Delete Request
                </Link>
              </Button>
            </div>
            <Filters
              selectedStatus={status}
              onStatusChange={setStatus}
              queryFilter={queryFilter}
              onQueryFilterChange={setQueryFilter}
            />
          </div>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            {error && (
              <Alert variant="destructive">
                <AlertCircle className="h-4 w-4" />
                <AlertTitle>Error</AlertTitle>
                <AlertDescription>{error.message}</AlertDescription>
              </Alert>
            )}

            {isLoading && (
              <div className="flex items-center justify-center p-8">
                <Loader2 className="h-16 w-16 animate-spin" />
              </div>
            )}

            {!isLoading && !error && filteredData && (
              <DeleteList
                requests={filteredData}
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

export default DeletesPage;
