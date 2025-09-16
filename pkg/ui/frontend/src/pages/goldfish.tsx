import { useState, useMemo, useEffect } from "react";
import { useSearchParams } from "react-router-dom";
import { useGoldfishQueries } from "@/hooks/use-goldfish-queries";
import { QueryDiffView } from "@/components/goldfish/query-diff-view";
import { TimeRangeSelector } from "@/components/goldfish/time-range-selector";
import { UserFilterCombobox } from "@/components/goldfish/user-filter-combobox";
import { TenantFilterSelect } from "@/components/goldfish/tenant-filter-select";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Card, CardHeader, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { RefreshCw, AlertCircle, CheckCircle2, XCircle, Rocket, ChevronDown } from "lucide-react";
import { OutcomeFilter, OUTCOME_ALL, OUTCOME_MATCH, OUTCOME_MISMATCH, OUTCOME_ERROR } from "@/types/goldfish";
import { PageContainer } from "@/layout/page-container";

// Define preset ranges (matching TimeRangeSelector)
const PRESET_RANGES = [
  { label: 'Last 15 minutes', value: '15m', duration: 15 * 60 * 1000 },
  { label: 'Last 30 minutes', value: '30m', duration: 30 * 60 * 1000 },
  { label: 'Last 1 hour', value: '1h', duration: 60 * 60 * 1000 },
  { label: 'Last 3 hours', value: '3h', duration: 3 * 60 * 60 * 1000 },
  { label: 'Last 6 hours', value: '6h', duration: 6 * 60 * 60 * 1000 },
  { label: 'Last 12 hours', value: '12h', duration: 12 * 60 * 60 * 1000 },
  { label: 'Last 24 hours', value: '24h', duration: 24 * 60 * 60 * 1000 },
  { label: 'Last 2 days', value: '2d', duration: 2 * 24 * 60 * 60 * 1000 },
  { label: 'Last 7 days', value: '7d', duration: 7 * 24 * 60 * 60 * 1000 },
];


export default function GoldfishPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  
  const [selectedTenant, setSelectedTenant] = useState<string | undefined>(() => {
    const tenant = searchParams.get("tenant");
    return tenant ? decodeURIComponent(tenant) : undefined;
  });
  const [selectedUser, setSelectedUser] = useState<string | undefined>(() => {
    const user = searchParams.get("user");
    return user ? decodeURIComponent(user) : undefined;
  });
  const [selectedOutcome, setSelectedOutcome] = useState<OutcomeFilter>(() => {
    const outcome = searchParams.get("outcome");
    if (outcome === OUTCOME_MATCH || outcome === OUTCOME_MISMATCH || outcome === OUTCOME_ERROR) {
      return outcome as OutcomeFilter;
    }
    return OUTCOME_ALL;
  });
  const [showNewEngineOnly, setShowNewEngineOnly] = useState(() => {
    return searchParams.get("newEngine") === "true";
  });
  
  // Initialize time range and error state
  const [timeRangeError, setTimeRangeError] = useState<string | null>(null);
  const [timeRange, setTimeRange] = useState<{ from: Date | null; to: Date | null }>(() => {
    const sinceParam = searchParams.get("since");
    const fromParam = searchParams.get("from");
    const toParam = searchParams.get("to");
    
    // Check for conflicting parameters
    if (sinceParam && (fromParam || toParam)) {
      setTimeRangeError("Conflicting time parameters: cannot use both 'since' and 'from/to' parameters.");
      return { from: null, to: null };
    }
    
    // Check for incomplete from/to range
    if ((fromParam && !toParam) || (!fromParam && toParam)) {
      setTimeRangeError("Incomplete time range: both 'from' and 'to' parameters are required.");
      return { from: null, to: null };
    }
    
    // Parse since parameter (relative time)
    if (sinceParam) {
      const match = sinceParam.match(/^(\d+)([mhd])$/);
      if (match) {
        const [, amount, unit] = match;
        const now = new Date();
        const msMultiplier = { m: 60 * 1000, h: 60 * 60 * 1000, d: 24 * 60 * 60 * 1000 }[unit] || 1;
        const from = new Date(now.getTime() - parseInt(amount) * msMultiplier);
        return { from, to: now };
      }
    }
    
    // Parse from/to parameters (absolute time)
    if (fromParam && toParam) {
      try {
        const from = new Date(fromParam);
        const to = new Date(toParam);
        if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
          return { from, to };
        }
      } catch {
        // Invalid date format, fall through to default
      }
    }
    
    // Default: no time range specified
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
  } = useGoldfishQueries(
    pageSize, 
    selectedOutcome, 
    selectedTenant, 
    selectedUser, 
    showNewEngineOnly ? true : undefined,
    timeRange.from,
    timeRange.to
  );
  
  // Store all seen tenants and users to maintain filters even when results are empty
  const [allSeenTenants, setAllSeenTenants] = useState<Set<string>>(new Set());
  const [allSeenUsers, setAllSeenUsers] = useState<Set<string>>(new Set());
  const [hasInitialData, setHasInitialData] = useState(false);
  
  // Fetch initial unfiltered data to populate filter options
  // This runs once on mount to get available tenants and users
  useEffect(() => {
    if (!hasInitialData && !isLoading) {
      // Use the queries from the initial load to populate filters
      if (queries.length > 0) {
        const tenants = new Set<string>();
        const users = new Set<string>();
        queries.forEach(q => {
          tenants.add(q.tenantId);
          if (q.user && q.user !== "unknown") {
            users.add(q.user);
          }
        });
        setAllSeenTenants(tenants);
        setAllSeenUsers(users);
        setHasInitialData(true);
      }
    }
  }, [queries, isLoading, hasInitialData]);
  
  // Continue to update seen tenants and users when we get new data
  useEffect(() => {
    if (queries.length > 0 && hasInitialData) {
      setAllSeenTenants(prev => {
        const newSet = new Set(prev);
        queries.forEach(q => newSet.add(q.tenantId));
        return newSet;
      });
      setAllSeenUsers(prev => {
        const newSet = new Set(prev);
        queries.forEach(q => {
          if (q.user && q.user !== "unknown") {
            newSet.add(q.user);
          }
        });
        return newSet;
      });
    }
  }, [queries, hasInitialData]);
  
  // Use all seen values for the dropdowns so they persist even when filtered
  const uniqueTenants = useMemo(() => {
    return Array.from(allSeenTenants).sort();
  }, [allSeenTenants]);
  
  const uniqueUsers = useMemo(() => {
    return Array.from(allSeenUsers).sort();
  }, [allSeenUsers]);

  // Update URL params when filter changes
  useEffect(() => {
    const newSearchParams = new URLSearchParams(searchParams);
    
    // Handle outcome filter
    if (selectedOutcome !== OUTCOME_ALL) {
      newSearchParams.set("outcome", selectedOutcome);
    } else {
      newSearchParams.delete("outcome");
    }
    
    // Handle tenant filter
    if (selectedTenant) {
      newSearchParams.set("tenant", selectedTenant);
    } else {
      newSearchParams.delete("tenant");
    }
    
    // Handle user filter
    if (selectedUser) {
      newSearchParams.set("user", selectedUser);
    } else {
      newSearchParams.delete("user");
    }
    
    // Handle time range filter
    // Clear all time-related params first
    newSearchParams.delete("since");
    newSearchParams.delete("from");
    newSearchParams.delete("to");
    
    if (timeRange.from && timeRange.to) {
      // Check if this matches a preset (within 1 second tolerance)
      const timeDiff = timeRange.to.getTime() - timeRange.from.getTime();
      const preset = PRESET_RANGES.find(p => Math.abs(timeDiff - p.duration) < 1000);
      
      if (preset) {
        // Use since parameter for presets
        newSearchParams.set("since", preset.value);
      } else {
        // Use from/to parameters for custom ranges
        newSearchParams.set("from", timeRange.from.toISOString());
        newSearchParams.set("to", timeRange.to.toISOString());
      }
    }
    
    // Handle newEngine filter
    if (showNewEngineOnly) {
      newSearchParams.set("newEngine", "true");
    } else {
      newSearchParams.delete("newEngine");
    }
    
    setSearchParams(newSearchParams, { replace: true });
  }, [selectedOutcome, selectedTenant, selectedUser, timeRange, showNewEngineOnly, searchParams, setSearchParams]);
  
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
              {/* Tenant Filter - With clear button */}
              {uniqueTenants.length > 0 && (
                <TenantFilterSelect
                  value={selectedTenant}
                  onChange={setSelectedTenant}
                  tenants={uniqueTenants}
                />
              )}
              
              {/* User Filter - Combobox with dropdown */}
              <UserFilterCombobox
                value={selectedUser}
                onChange={setSelectedUser}
                suggestions={uniqueUsers}
              />
              
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
            {timeRangeError && (
              <Alert variant="destructive">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription>
                  {timeRangeError}
                </AlertDescription>
              </Alert>
            )}
            
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
              <>
                <div className="text-center py-12 text-muted-foreground">
                  <p>No queries found in current results
                  {selectedTenant && ` for tenant ${selectedTenant}`}
                  {selectedUser && ` for user ${selectedUser}`}</p>
                  {hasMore && (
                    <p className="mt-2">
                      Try loading more results to find matching queries
                    </p>
                  )}
                </div>
                
                {/* Show Load More button even when no queries match current filter */}
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
                          Load More Results
                        </>
                      )}
                    </Button>
                  </div>
                )}
              </>
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
                      <span>
                        No more results ({queries.length} total queries)
                      </span>
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
