"use client";

import { Check, AlertCircle } from "lucide-react";
import { useLogLevel } from "@/hooks/use-log-level";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

const LOG_LEVELS = ["debug", "info", "warn", "error"] as const;

interface LogLevelSelectProps {
  nodeName: string;
  className?: string;
}

export function LogLevelSelect({ nodeName, className }: LogLevelSelectProps) {
  const { logLevel, isLoading, error, success, setLogLevel } =
    useLogLevel(nodeName);

  const handleValueChange = (value: string) => {
    setLogLevel(value);
  };

  return (
    <div className="relative flex items-center gap-2">
      <Select
        value={logLevel}
        onValueChange={handleValueChange}
        disabled={isLoading}
      >
        <SelectTrigger
          className={cn(
            "w-[180px]",
            className,
            isLoading && "opacity-50 cursor-not-allowed"
          )}
        >
          <SelectValue placeholder="Select log level" />
        </SelectTrigger>
        <SelectContent>
          {LOG_LEVELS.map((level) => (
            <SelectItem key={level} value={level}>
              {level}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* Success/Error Indicator with Tooltip */}
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <div
              className={cn(
                "absolute -right-6 transition-all duration-300 ease-in-out",
                success || error
                  ? "opacity-100 translate-x-0"
                  : "opacity-0 translate-x-2"
              )}
            >
              {success && (
                <Check className="h-4 w-4 text-green-500 animate-in zoom-in-50 duration-300" />
              )}
              {error && (
                <AlertCircle className="h-4 w-4 text-red-500 animate-in zoom-in-50 duration-300" />
              )}
            </div>
          </TooltipTrigger>
          <TooltipContent side="right" className="text-xs">
            {success && "Log level updated successfully"}
            {error && error}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </div>
  );
}
