import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface ReadinessIndicatorProps {
  isReady?: boolean;
  message?: string;
  className?: string;
}

export function ReadinessIndicator({
  isReady,
  message,
  className,
}: ReadinessIndicatorProps) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div className={cn("flex items-center gap-2", className)}>
            <div
              className={cn(
                "h-2.5 w-2.5 rounded-full",
                isReady ? "bg-green-500" : "bg-red-500"
              )}
            />
          </div>
        </TooltipTrigger>
        <TooltipContent>
          <p className="text-sm">
            {message || (isReady ? "Ready" : "Not Ready")}
          </p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
