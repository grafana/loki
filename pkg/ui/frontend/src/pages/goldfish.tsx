import { useState, useMemo, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { useGoldfishQueries } from "@/hooks/use-goldfish-queries";
import { QueryDiffView } from "@/components/goldfish/query-diff-view";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Checkbox } from "@/components/ui/checkbox";
import { RefreshCw, AlertCircle, CheckCircle2, XCircle, Rocket } from "lucide-react";
import { OutcomeFilter, OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR, SampledQuery } from "@/types/goldfish";
import { PageContainer } from "@/layout/page-container";
import { filterQueriesByOutcome } from "@/lib/goldfish-utils";


export default function GoldfishPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedUser, setSelectedUser] = useState<string | undefined>(undefined);
  const [selectedOutcome, setSelectedOutcome] = useState<OutcomeFilter>(OUTCOME_ALL);
  const [showNewEngineOnly, setShowNewEngineOnly] = useState(() => {
    return searchParams.get("newEngine") === "true";
  });
  const [page, setPage] = useState(1);
  const pageSize = 10; // Reduced since we're showing more detail per query
  
  const { data, isLoading, error, refetch, totalPages } = useGoldfishQueries(
    page, 
    pageSize, 
    selectedOutcome, 
    selectedTenant, 
    selectedUser, 
    showNewEngineOnly ? true : undefined
  );
  const allQueries = useMemo(() => (data as { queries: SampledQuery[] })?.queries || [], [data]);
  
  // We need a separate query to get all unique tenants and users
  // For now, we'll use the current page's data, but ideally this would be a separate endpoint
  const uniqueTenants = useMemo(() => {
    const tenants = new Set(allQueries.map((q) => q.tenantId));
    return Array.from(tenants).sort();
  }, [allQueries]);
  
  const uniqueUsers = useMemo(() => {
    const users = new Set(allQueries.map((q) => q.user).filter((u) => u && u !== "unknown"));
    return Array.from(users).sort();
  }, [allQueries]);
  
  // Queries are now filtered on the backend, no need for client-side filtering
  const filteredQueries = allQueries;

  // Update URL params when filter changes
  useEffect(() => {
    const newSearchParams = new URLSearchParams(searchParams);
    if (showNewEngineOnly) {
      newSearchParams.set("newEngine", "true");
    } else {
      newSearchParams.delete("newEngine");
    }
    setSearchParams(newSearchParams, { replace: true });
  }, [showNewEngineOnly, searchParams, setSearchParams]);
  
  return (
    <PageContainer>
      <Card className="shadow-sm">
        <CardHeader>
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
          
          {/* All Filters on One Line */}
          <div className="flex items-center justify-between gap-4 mb-6">
            {/* Outcome Filter Tabs - Left Aligned */}
            <div className="flex items-center bg-muted p-1 rounded-lg">
              <Button
                variant={selectedOutcome === OUTCOME_ALL ? "default" : "ghost"}
                size="sm"
                onClick={() => {
                  setSelectedOutcome(OUTCOME_ALL);
                  setPage(1);
                }}
                disabled={isLoading}
                className="text-sm"
              >
                All
              </Button>
              <Button
                variant={selectedOutcome === OUTCOME_MATCH ? "default" : "ghost"}
                size="sm"
                onClick={() => {
                  setSelectedOutcome(OUTCOME_MATCH);
                  setPage(1);
                }}
                disabled={isLoading}
                className="text-sm"
              >
                <CheckCircle2 className="h-4 w-4 mr-1" />
                Match
              </Button>
              <Button
                variant={selectedOutcome === OUTCOME_MISMATCH ? "default" : "ghost"}
                size="sm"
                onClick={() => {
                  setSelectedOutcome(OUTCOME_MISMATCH);
                  setPage(1);
                }}
                disabled={isLoading}
                className="text-sm"
              >
                <XCircle className="h-4 w-4 mr-1" />
                Mismatch
              </Button>
              <Button
                variant={selectedOutcome === OUTCOME_ERROR ? "default" : "ghost"}
                size="sm"
                onClick={() => {
                  setSelectedOutcome(OUTCOME_ERROR);
                  setPage(1);
                }}
                disabled={isLoading}
                className="text-sm"
              >
                <AlertCircle className="h-4 w-4 mr-1" />
                Error
              </Button>
            </div>
            
            {/* Other Filters - Right Aligned */}
            <div className="flex items-center gap-4">
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
                {uniqueTenants.map((tenant) => (
                  <SelectItem key={tenant} value={tenant}>
                    {tenant}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            
            <Select
              value={selectedUser}
              onValueChange={setSelectedUser}
              disabled={isLoading || uniqueUsers.length === 0}
            >
              <SelectTrigger className="w-[200px]">
                <SelectValue placeholder="Filter by user" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Users</SelectItem>
                {uniqueUsers.map((user) => (
                  <SelectItem key={user} value={user}>
                    {user}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            
            <div className="flex items-center space-x-2">
              <Checkbox 
                id="new-engine-only"
                checked={showNewEngineOnly}
                onCheckedChange={(checked) => {
                  setShowNewEngineOnly(checked as boolean);
                  setPage(1);
                }}
                disabled={isLoading}
              />
              <label 
                htmlFor="new-engine-only"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 flex items-center cursor-pointer"
              >
                <Rocket className="h-4 w-4 mr-1" />
                New Engine Only
              </label>
            </div>
            </div>
          </div>
        </CardHeader>
        
        <CardContent>
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
                {selectedTenant && ` for tenant ${selectedTenant}`}
                {selectedUser && ` for user ${selectedUser}`}
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
        </CardContent>
      </Card>
    </PageContainer>
  );
}
