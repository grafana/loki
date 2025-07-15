import React from "react";
import { ServiceState } from "../../types/cluster";
import { Badge } from "@/components/ui/badge";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";

interface StatusBadgeProps {
  services: ServiceState[];
  error?: string;
}

const StatusBadge: React.FC<StatusBadgeProps> = ({ services, error }) => {
  const getStatusInfo = () => {
    if (error) {
      return {
        className:
          "bg-red-500 dark:bg-red-500/80 hover:bg-red-600 dark:hover:bg-red-500 text-white border-transparent",
        tooltip: `Error: ${error}`,
        status: "error",
      };
    }

    const allRunning = services.every((s) => s.status === "Running");
    const onlyStartingOrRunning = services.every(
      (s) => s.status === "Starting" || s.status === "Running"
    );

    if (allRunning) {
      return {
        className:
          "bg-green-500 dark:bg-green-500/80 hover:bg-green-600 dark:hover:bg-green-500 text-white border-transparent",
        status: "healthy",
      };
    } else if (onlyStartingOrRunning) {
      return {
        className:
          "bg-yellow-500 dark:bg-yellow-500/80 hover:bg-yellow-600 dark:hover:bg-yellow-500 text-white border-transparent",
        status: "pending",
      };
    } else {
      return {
        className:
          "bg-red-500 dark:bg-red-500/80 hover:bg-red-600 dark:hover:bg-red-500 text-white border-transparent",
        status: "unhealthy",
      };
    }
  };

  const getStatusColor = (status: string) => {
    switch (status) {
      case "Running":
        return "text-green-600 dark:text-green-400";
      case "Starting":
        return "text-yellow-600 dark:text-yellow-400";
      case "Failed":
        return "text-red-600 dark:text-red-400";
      case "Terminated":
        return "text-gray-600 dark:text-gray-400";
      case "Stopping":
        return "text-orange-600 dark:text-orange-400";
      case "New":
        return "text-blue-600 dark:text-blue-400";
      default:
        return "text-gray-600 dark:text-gray-400";
    }
  };

  const { className } = getStatusInfo();

  return (
    <HoverCard>
      <HoverCardTrigger>
        <button type="button">
          <Badge className={className}>{services.length} services</Badge>
        </button>
      </HoverCardTrigger>
      <HoverCardContent
        className="w-80 rounded-md border bg-popover p-4 text-popover-foreground shadow-md outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2"
        sideOffset={5}
      >
        <div className="space-y-2">
          <div className="font-medium border-b border-gray-200 dark:border-gray-700 pb-1">
            Service Status
          </div>
          <div className="space-y-1">
            {services.map((service, idx) => (
              <div key={idx} className="flex justify-between items-center">
                <span className="mr-4 font-medium">{service.service}</span>
                <span className={`${getStatusColor(service.status)}`}>
                  {service.status}
                </span>
              </div>
            ))}
          </div>
          {error && (
            <div className="mt-2 pt-2 border-t border-gray-200 dark:border-gray-700 text-red-600 dark:text-red-400">
              {error}
            </div>
          )}
        </div>
      </HoverCardContent>
    </HoverCard>
  );
};

export default StatusBadge;
