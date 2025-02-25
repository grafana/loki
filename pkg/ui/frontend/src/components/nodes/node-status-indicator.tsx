import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";
import { absolutePath } from "@/util";

interface NodeStatusIndicatorProps {
  nodeName: string;
  className?: string;
}

interface NodeStatus {
  isReady: boolean;
  message: string;
}

export function NodeStatusIndicator({
  nodeName,
  className,
}: NodeStatusIndicatorProps) {
  const [status, setStatus] = useState<NodeStatus>({
    isReady: false,
    message: "Checking status...",
  });
  const [isVisible, setIsVisible] = useState(true);

  useEffect(() => {
    const checkStatus = async () => {
      try {
        const response = await fetch(
          absolutePath(`/api/v1/proxy/${nodeName}/ready`)
        );
        const text = await response.text();
        setStatus({
          isReady: response.ok && text.includes("ready"),
          message: response.ok ? "Ready" : text,
        });
      } catch (error) {
        setStatus({
          isReady: false,
          message:
            error instanceof Error ? error.message : "Failed to check status",
        });
      }
    };

    // Initial check
    checkStatus();

    // Set up the status check interval
    const statusInterval = setInterval(checkStatus, 3000);

    // Set up the blink interval
    const blinkInterval = setInterval(() => {
      setIsVisible((prev) => !prev);
    }, 1000);

    // Cleanup intervals on unmount
    return () => {
      clearInterval(statusInterval);
      clearInterval(blinkInterval);
    };
  }, [nodeName]);

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <span
        className={cn(
          "text-sm",
          status.isReady ? "text-muted-foreground" : "text-red-500"
        )}
      >
        {status.message}
      </span>
      <div
        className={cn(
          "h-2.5 w-2.5 rounded-full transition-opacity duration-150",
          status.isReady ? "bg-green-500" : "bg-red-500",
          isVisible ? "opacity-100" : "opacity-30"
        )}
      />
    </div>
  );
}
