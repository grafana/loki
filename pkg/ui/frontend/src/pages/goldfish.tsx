import { useState, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { fetchSampledQueries } from "@/lib/goldfish-api";
import { QueryDiffView } from "@/components/goldfish/query-diff-view";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { RefreshCw, AlertCircle } from "lucide-react";


export default function GoldfishPage() {
  const [selectedTenant, setSelectedTenant] = useState<string>("all");
  const [page, setPage] = useState(1);
  const pageSize = 10; // Reduced since we're showing more detail per query
  
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["goldfish-queries", page, pageSize],
    queryFn: () => fetchSampledQueries(page, pageSize),
    refetchInterval: 30000, // Refresh every 30 seconds
  });
  
  const allQueries = data?.queries || [];
  
  // Extract unique tenants from queries
  const uniqueTenants = useMemo(() => {
    const tenants = new Set(allQueries.map(q => q.tenantId));
    return Array.from(tenants).sort();
  }, [allQueries]);
  
  // Filter queries based on tenant
  const filteredQueries = allQueries.filter(query => {
    const matchesTenant = selectedTenant === "all" || query.tenantId === selectedTenant;
    return matchesTenant;
  });
  
  const totalPages = data ? Math.ceil(data.total / pageSize) : 0;
  
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Goldfish - Query Comparison</h1>
          <p className="text-muted-foreground mt-1">
            Side-by-side performance comparison between Cell A and Cell B
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isLoading}
        >
          <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>
      
      <div className="flex items-center justify-end gap-4 mb-6">
        <Select
          value={selectedTenant}
          onValueChange={setSelectedTenant}
          disabled={isLoading || uniqueTenants.length === 0}
        >
          <SelectTrigger className="w-[200px]">
            <SelectValue placeholder="Filter by tenant" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Tenants</SelectItem>
            {uniqueTenants.map(tenant => (
              <SelectItem key={tenant} value={tenant}>
                {tenant}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      
      <div className="space-y-4">
          {error && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>
                Failed to load queries: {(error as Error).message}
              </AlertDescription>
            </Alert>
          )}
          
          {isLoading ? (
            // Loading skeletons
            <div className="space-y-4">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-96 w-full" />
              ))}
            </div>
          ) : filteredQueries.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              No queries found
              {selectedTenant !== "all" && ` for tenant ${selectedTenant}`}
            </div>
          ) : (
            <div className="space-y-4">
              {filteredQueries.map((query) => (
                <QueryDiffView key={query.correlationId} query={query} />
              ))}
            </div>
          )}
          
          {totalPages > 1 && !isLoading && (
            <div className="flex items-center justify-between pt-4">
              <div className="text-sm text-muted-foreground">
                Page {page} of {totalPages}
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setPage(p => Math.max(1, p - 1));
                    window.scrollTo({ top: 0, behavior: 'smooth' });
                  }}
                  disabled={page === 1}
                >
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setPage(p => Math.min(totalPages, p + 1));
                    window.scrollTo({ top: 0, behavior: 'smooth' });
                  }}
                  disabled={page === totalPages}
                >
                  Next
                </Button>
              </div>
            </div>
          )}
      </div>
    </div>
  );
}
