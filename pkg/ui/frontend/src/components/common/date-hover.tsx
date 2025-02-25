import React from "react";
import { formatDistanceToNow, format } from "date-fns";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
interface DateHoverProps {
  date: Date;
  className?: string;
}

export const DateHover: React.FC<DateHoverProps> = ({
  date,
  className = "",
}) => {
  const relativeTime = formatDistanceToNow(date, { addSuffix: true });
  const localTime = format(date, "yyyy-MM-dd HH:mm:ss");
  const utcTime = format(
    new Date(date.getTime() + date.getTimezoneOffset() * 60000),
    "yyyy-MM-dd HH:mm:ss"
  );

  return (
    <HoverCard>
      <HoverCardTrigger>
        <div className={`inline-block ${className}`}>{relativeTime}</div>
      </HoverCardTrigger>
      <HoverCardContent className="w-[280px]">
        <div className="space-y-2">
          <div className="flex items-center gap-3">
            <span className="px-2 py-0.5 text-xs font-medium bg-gray-100 rounded dark:bg-gray-700 w-14 text-center">
              UTC
            </span>
            <span className="font-mono">{utcTime}</span>
          </div>
          <div className="flex items-center gap-3">
            <span className="px-2 py-0.5 text-xs font-medium bg-gray-100 rounded dark:bg-gray-700 w-14 text-center">
              Local
            </span>
            <span className="font-mono">{localTime}</span>
          </div>
        </div>
      </HoverCardContent>
    </HoverCard>
  );
};
