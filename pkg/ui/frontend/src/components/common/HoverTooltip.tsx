import React from "react";
import { createPortal } from "react-dom";

interface HoverTooltipProps {
  children: React.ReactNode;
  content: React.ReactNode;
  className?: string;
  delay?: number;
  maxHeight?: number;
}

export const HoverTooltip: React.FC<HoverTooltipProps> = ({
  children,
  content,
  className = "",
  delay = 150,
  maxHeight = 400,
}) => {
  const [isHovered, setIsHovered] = React.useState(false);
  const [position, setPosition] = React.useState({ top: 0, left: 0 });
  const triggerRef = React.useRef<HTMLDivElement>(null);
  const tooltipRef = React.useRef<HTMLDivElement>(null);
  const timeoutRef = React.useRef<number>();

  const updatePosition = React.useCallback(() => {
    if (triggerRef.current && tooltipRef.current) {
      const triggerRect = triggerRef.current.getBoundingClientRect();
      const tooltipRect = tooltipRef.current.getBoundingClientRect();
      const windowWidth = window.innerWidth;
      const windowHeight = window.innerHeight;

      // Calculate initial position
      let top = triggerRect.top + window.scrollY - 70;
      let left = triggerRect.left + window.scrollX;

      // Check if tooltip would go off the right edge
      if (left + tooltipRect.width > windowWidth - 20) {
        // Position tooltip to the left of the trigger
        left = triggerRect.left - tooltipRect.width - 10;
        if (left < 20) {
          // If there's not enough space on the left either,
          // position it at the right edge of the window
          left = windowWidth - tooltipRect.width - 20;
        }
      }

      // Check if tooltip would go off the bottom edge
      if (top + tooltipRect.height > windowHeight + window.scrollY - 20) {
        // Position tooltip above the trigger
        top = triggerRect.top + window.scrollY - tooltipRect.height - 10;
      }

      setPosition({ top, left });
    }
  }, []);

  const handleMouseEnter = React.useCallback(() => {
    clearTimeout(timeoutRef.current);
    timeoutRef.current = window.setTimeout(() => {
      setIsHovered(true);
    }, delay);
  }, [delay]);

  const handleMouseLeave = React.useCallback((e: MouseEvent) => {
    const tooltipEl = tooltipRef.current;
    const triggerEl = triggerRef.current;

    // Check if we're moving to the tooltip
    if (
      tooltipEl &&
      e.relatedTarget instanceof Node &&
      tooltipEl.contains(e.relatedTarget)
    ) {
      return;
    }

    // Check if we're moving from the tooltip back to trigger
    if (
      triggerEl &&
      e.relatedTarget instanceof Node &&
      triggerEl.contains(e.relatedTarget)
    ) {
      return;
    }

    clearTimeout(timeoutRef.current);
    setIsHovered(false);
  }, []);

  React.useEffect(() => {
    if (isHovered) {
      // Use requestAnimationFrame to ensure the tooltip is rendered before measuring
      requestAnimationFrame(() => {
        updatePosition();
      });
      window.addEventListener("scroll", updatePosition);
      window.addEventListener("resize", updatePosition);
      document.addEventListener("mouseleave", handleMouseLeave);
    }
    return () => {
      window.removeEventListener("scroll", updatePosition);
      window.removeEventListener("resize", updatePosition);
      document.removeEventListener("mouseleave", handleMouseLeave);
    };
  }, [isHovered, updatePosition, handleMouseLeave]);

  React.useEffect(() => {
    return () => {
      clearTimeout(timeoutRef.current);
    };
  }, []);

  return (
    <>
      <div
        ref={triggerRef}
        className={`inline-block ${className}`}
        onMouseEnter={handleMouseEnter}
        onMouseLeave={handleMouseLeave as any}
      >
        {children}
      </div>
      {isHovered &&
        createPortal(
          <div
            ref={tooltipRef}
            style={{
              position: "absolute",
              top: `${position.top}px`,
              left: `${position.left}px`,
              maxHeight: `${maxHeight}px`,
            }}
            className="z-[9999] min-w-[280px] max-w-md text-sm text-gray-500 bg-white border border-gray-200 rounded-lg shadow-sm dark:text-gray-400 dark:border-gray-600 dark:bg-gray-800 overflow-y-auto"
            onMouseEnter={handleMouseEnter}
            onMouseLeave={handleMouseLeave as any}
          >
            <div className="px-3 py-2">{content}</div>
          </div>,
          document.body
        )}
    </>
  );
};

export default HoverTooltip;
