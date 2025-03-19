import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Loader2, Pause } from "lucide-react";

interface RefreshLoopProps {
  onRefresh: () => void;
  isPaused?: boolean;
  isLoading: boolean;
  className?: string;
}

export function RefreshLoop({
  onRefresh,
  isPaused = false,
  isLoading,
  className,
}: RefreshLoopProps) {
  const [delayedLoading, setDelayedLoading] = useState(isLoading);

  useEffect(() => {
    let timeoutId: NodeJS.Timeout;
    if (isLoading) {
      setDelayedLoading(true);
    } else {
      timeoutId = setTimeout(() => {
        setDelayedLoading(false);
      }, 1000); // Keep loading state for 1 second after isLoading becomes false
    }
    return () => {
      if (timeoutId) clearTimeout(timeoutId);
    };
  }, [isLoading]);

  return (
    <div
      className={`flex items-center gap-2 text-sm text-muted-foreground ${className}`}
    >
      <Button
        variant="secondary"
        size="sm"
        className="h-6 px-2 text-xs hover:bg-muted"
        onClick={onRefresh}
      >
        Refresh now
      </Button>
      {isPaused ? (
        <Pause className="h-3 w-3 text-orange-500" />
      ) : (
        <Loader2
          className={`h-3 w-3 ${
            delayedLoading
              ? "animate-spin text-emerald-500 "
              : "opacity-0 transition-opacity duration-1000"
          } `}
        />
      )}
      <span className="transition-opacity duration-1000">
        {isPaused
          ? "Auto-refresh paused"
          : delayedLoading
          ? "Refreshing..."
          : ``}
      </span>
    </div>
  );
}
