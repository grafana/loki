import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Loader2, Pause } from "lucide-react";
import { cn } from "@/lib/utils";

interface RingStatsProps {
  nextRefresh: number;
  isPaused: boolean;
  onRefresh: () => void;
  instancesByState: Record<string, number>;
}

export function RingStats({
  nextRefresh,
  isPaused,
  onRefresh,
  instancesByState,
}: RingStatsProps) {
  const activeInstances = instancesByState["ACTIVE"] || 0;
  const inactiveInstances = Object.entries(instancesByState)
    .filter(([state]) => state !== "ACTIVE")
    .reduce((sum, [_, count]) => sum + count, 0);

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
      <Card>
        <CardHeader className="pb-2">
          <div className="flex items-center justify-between">
            <CardTitle className="text-sm font-medium text-muted-foreground">
              Next Ring Update
            </CardTitle>
            <Button
              variant="ghost"
              size="sm"
              onClick={onRefresh}
              className="h-8 w-8 p-0"
              title={isPaused ? "Auto-refresh paused" : "Refresh ring data"}
            >
              {isPaused ? (
                <Pause className="h-4 w-4 text-orange-500" />
              ) : (
                <Loader2 className="h-4 w-4 text-green-500 animate-spin" />
              )}
              <span className="sr-only">
                {isPaused ? "Auto-refresh paused" : "Refresh ring data"}
              </span>
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <div
            className={cn(
              "text-2xl font-bold",
              isPaused ? "text-orange-500" : ""
            )}
          >
            {isPaused ? "Paused" : `${nextRefresh}s`}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Active Instances
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-green-600 dark:text-green-400">
            {activeInstances}
          </div>
        </CardContent>
      </Card>
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            Inactive Instances
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold text-red-600 dark:text-red-400">
            {inactiveInstances}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
