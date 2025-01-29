import React from "react";
import { ServiceState } from "../../types";
import HoverTooltip from "../common/HoverTooltip";

interface StatusBadgeProps {
  services: ServiceState[];
  error?: string;
}

const StatusBadge: React.FC<StatusBadgeProps> = ({ services, error }) => {
  const getStatusInfo = () => {
    if (error) {
      return {
        color: "bg-red-100 text-red-800",
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
        color: "bg-green-100 text-green-800",
        status: "healthy",
      };
    } else if (onlyStartingOrRunning) {
      return {
        color: "bg-yellow-100 text-yellow-800",

        status: "pending",
      };
    } else {
      return {
        color: "bg-red-100 text-red-800",
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

  const { color } = getStatusInfo();

  const renderServicesList = () => {
    return (
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
    );
  };

  return (
    <HoverTooltip content={renderServicesList()}>
      <span
        className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${color}`}
      >
        {services.length} services
      </span>
    </HoverTooltip>
  );
};

export default StatusBadge;
