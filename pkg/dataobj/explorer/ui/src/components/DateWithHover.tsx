import React from "react";
import { formatDistanceToNow, format } from "date-fns";

interface DateWithHoverProps {
  date: Date;
  className?: string;
}

export const DateWithHover: React.FC<DateWithHoverProps> = ({
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
    <div className={className} title={`UTC: ${utcTime}\nLocal: ${localTime}`}>
      {relativeTime}
    </div>
  );
};
