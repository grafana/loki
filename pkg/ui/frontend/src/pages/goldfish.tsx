import { useState, useMemo, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { useGoldfishQueriesLoadMore } from "@/hooks/use-goldfish-queries-loadmore";
import { QueryDiffView } from "@/components/goldfish/query-diff-view";
import { TimeRangeSelector } from "@/components/goldfish/time-range-selector";
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
import { Input } from "@/components/ui/input";
import { RefreshCw, AlertCircle, CheckCircle2, XCircle, Rocket, ChevronDown } from "lucide-react";
import { OutcomeFilter, OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR } from "@/types/goldfish";
import { PageContainer } from "@/layout/page-container";


export default function GoldfishPageLoadMore() {
  const [searchParams, setSearchParams] = useSearchParams();
  
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(undefined);
  const [selectedUser, setSelectedUser] = useState<string | undefined>(undefined);
  const [selectedOutcome, setSelectedOutcome] = useState<OutcomeFilter>(OUTCOME_ALL);
  const [showNewEngineOnly, setShowNewEngineOnly] = useState(() => {
    return searchParams.get("newEngine") === "true";
  });
  
  // Initialize time range to null (use backend default)
  const [timeRange, setTimeRange] = useState<{ from: Date | null; to: Date | null }>(() => {
    return { from: null, to: null };
  });
  
  const pageSize = 20; // Increased since we're using load more pattern
  
  const { 
    queries, 
    isLoading, 
    isLoadingMore,
    error, 
    hasMore, 
    loadMore, 
    refresh, 
    traceId 
  } = useGoldfishQueriesLoadMore(
    pageSize, 
    selectedOutcome, 
    selectedTenant, 
    selectedUser, 
    showNewEngineOnly ? true : undefined,
    timeRange.from,
    timeRange.to
  );
  
  // We need a separate query to get all unique tenants and users
  // For now, we'll use the current data, but ideally this would be a separate endpoint
  const uniqueTenants = useMemo(() => {
    const tenants = new Set(queries.map((q) => q.tenantId));
    return Array.from(tenants).sort();
  }, [queries]);
  
  const uniqueUsers = useMemo(() => {
    const users = new Set(queries.map((q) => q.user).filter((u) => u && u !== "unknown"));
    return Array.from(users).sort();
  }, [queries]);

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
              onClick={refresh}
              disabled={isLoading}
            >
              <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
              Refresh
            </Button>
          </div>
          
          {/* All Filters on Two Lines */}
          <div className="space-y-3 mb-6">
            {/* First Line: Time Range and Refresh */}
            <div className="flex items-center justify-end gap-4">
              <div className="text-sm text-muted-foreground">
                Showing queries from:
              </div>
              <TimeRangeSelector
                from={timeRange.from}
                to={timeRange.to}
                onChange={(from, to) => setTimeRange({ from, to })}
              />
            </div>
            
            {/* Second Line: Outcome Filter and Other Filters */}
            <div className="flex items-center justify-between gap-4">
              {/* Outcome Filter Tabs - Left Aligned */}
              <div className="flex items-center bg-muted p-1 rounded-lg">
              <Button
                variant={selectedOutcome === OUTCOME_ALL ? "default" : "ghost"}
                size="sm"
                onClick={() => setSelectedOutcome(OUTCOME_ALL)}
                className="px-3 py-1"
              >
                All
              </Button>
              <Button
                variant={selectedOutcome === OUTCOME_MATCH ? "default" : "ghost"}
                size="sm"
                onClick={() => setSelectedOutcome(OUTCOME_MATCH)}
                className="px-3 py-1"
              >
                <CheckCircle2 className="h-3 w-3 mr-1" />
                Match
              </Button>
              <Button
                variant={selectedOutcome === OUTCOME_MISMATCH ? "default" : "ghost"}
                size="sm"
                onClick={() => setSelectedOutcome(OUTCOME_MISMATCH)}
                className="px-3 py-1"
              >
                <XCircle className="h-3 w-3 mr-1" />
                Mismatch
              </Button>
              <Button
                variant={selectedOutcome === OUTCOME_ERROR ? "default" : "ghost"}
                size="sm"
                onClick={() => setSelectedOutcome(OUTCOME_ERROR)}
                className="px-3 py-1"
              >
                <AlertCircle className="h-3 w-3 mr-1" />
                Error
              </Button>
            </div>
            
            {/* Other Filters - Right Aligned */}
            <div className="flex items-center gap-3">
              {/* Tenant Filter */}
              {uniqueTenants.length > 0 && (
                <Select
                  value={selectedTenant || "all"}
                  onValueChange={(value) => setSelectedTenant(value === "all" ? undefined : value)}
                >
                  <SelectTrigger className="w-[140px] h-8">
                    <SelectValue placeholder="Tenant" />
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
              )}
              
              {/* User Filter - Always visible with manual entry */}
              <div className="relative">
                <Input
                  type="text"
                  placeholder="Filter by user..."
                  value={selectedUser || ""}
                  onChange={(e) => setSelectedUser(e.target.value || undefined)}
                  className="w-[180px] h-8"
                  list="user-suggestions"
                />
                {uniqueUsers.length > 0 && (
                  <datalist id="user-suggestions">
                    {uniqueUsers.map((user) => (
                      <option key={user} value={user} />
                    ))}
                  </datalist>
                )}
              </div>
              
              {/* New Engine Filter */}
              <div className="flex items-center space-x-2">
              <Checkbox
                id="new-engine"
                checked={showNewEngineOnly}
                onCheckedChange={(checked) => setShowNewEngineOnly(checked as boolean)}
              />
              <label
                htmlFor="new-engine"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70 flex items-center cursor-pointer"
              >
                <Rocket className="h-4 w-4 mr-1" />
                New Engine Only
              </label>
              </div>
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
                  <div className="space-y-2">
                    <div>Failed to load queries: {(error as Error).message}</div>
                    {traceId && (
                      <div className="text-xs">
                        <span className="font-semibold">Trace ID: </span>
                        <code className="bg-destructive/10 px-1 py-0.5 rounded">{traceId}</code>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="ml-2 h-6 px-2 text-xs"
                          onClick={() => {
                            // Copy trace ID to clipboard
                            navigator.clipboard.writeText(traceId);
                          }}
                        >
                          Copy
                        </Button>
                      </div>
                    )}
                  </div>
                </AlertDescription>
              </Alert>
            )}
            
            {isLoading && queries.length === 0 ? (
              // Initial loading skeletons
              <div className="space-y-4">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-96 w-full" />
                ))}
              </div>
            ) : queries.length === 0 ? (
              <div className="text-center py-12 text-muted-foreground">
                No queries found
                {selectedTenant && ` for tenant ${selectedTenant}`}
                {selectedUser && ` for user ${selectedUser}`}
              </div>
            ) : (
              <>
                <div className="space-y-4">
                  {queries.map((query) => (
                    <QueryDiffView key={query.correlationId} query={query} />
                  ))}
                </div>
                
                {/* Load More Section */}
                {hasMore && (
                  <div className="flex justify-center pt-6">
                    <Button
                      variant="outline"
                      size="lg"
                      onClick={loadMore}
                      disabled={isLoadingMore}
                      className="min-w-[200px]"
                    >
                      {isLoadingMore ? (
                        <>
                          <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                          Loading...
                        </>
                      ) : (
                        <>
                          <ChevronDown className="h-4 w-4 mr-2" />
                          Load More
                        </>
                      )}
                    </Button>
                  </div>
                )}
                
                {/* Loading indicator for more items */}
                {isLoadingMore && (
                  <div className="space-y-4">
                    {Array.from({ length: 2 }).map((_, i) => (
                      <Skeleton key={`loading-${i}`} className="h-96 w-full" />
                    ))}
                  </div>
                )}
                
                {/* End of results indicator */}
                {!hasMore && queries.length > 0 && (
                  <div className="text-center py-6 text-muted-foreground text-sm">
                    <div className="flex items-center justify-center gap-2">
                      <div className="h-px bg-border flex-1 max-w-xs" />
                      <span>End of results ({queries.length} queries)</span>
                      <div className="h-px bg-border flex-1 max-w-xs" />
                    </div>
                  </div>
                )}
              </>
            )}
          </div>
        </CardContent>
      </Card>
    </PageContainer>
  );
}