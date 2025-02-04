import React from "react";
import { formatDistanceToNow, format } from "date-fns";
import { createPortal } from "react-dom";

interface DateWithHoverProps {
  date: Date;
  className?: string;
}

export const DateWithHover: React.FC<DateWithHoverProps> = ({
  date,
  className = "",
}) => {
  const [isHovered, setIsHovered] = React.useState(false);
  const relativeTime = formatDistanceToNow(date, { addSuffix: true });
  const localTime = format(date, "yyyy-MM-dd HH:mm:ss");
  const utcTime = format(
    new Date(date.getTime() + date.getTimezoneOffset() * 60000),
    "yyyy-MM-dd HH:mm:ss"
  );

  const [position, setPosition] = React.useState({ top: 0, left: 0 });
  const triggerRef = React.useRef<HTMLDivElement>(null);

  const updatePosition = React.useCallback(() => {
    if (triggerRef.current) {
      const rect = triggerRef.current.getBoundingClientRect();
      setPosition({
        top: rect.top + window.scrollY - 70, // Position above the element
        left: rect.left + window.scrollX,
      });
    }
  }, []);

  React.useEffect(() => {
    if (isHovered) {
      updatePosition();
      window.addEventListener("scroll", updatePosition);
      window.addEventListener("resize", updatePosition);
    }
    return () => {
      window.removeEventListener("scroll", updatePosition);
      window.removeEventListener("resize", updatePosition);
    };
  }, [isHovered, updatePosition]);

  return (
    <>
      <div
        ref={triggerRef}
        className={`inline-block ${className}`}
        onMouseEnter={() => setIsHovered(true)}
        onMouseLeave={() => setIsHovered(false)}
      >
        {relativeTime}
      </div>
      {isHovered &&
        createPortal(
          <div
            style={{
              position: "absolute",
              top: `${position.top}px`,
              left: `${position.left}px`,
            }}
            className="z-[9999] min-w-[280px] text-sm text-gray-500 bg-white border border-gray-200 rounded-lg shadow-sm dark:text-gray-400 dark:border-gray-600 dark:bg-gray-800"
          >
            <div className="px-3 py-2 space-y-2">
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
          </div>,
          document.body
        )}
    </>
  );
};
