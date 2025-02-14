import { useMemo, useRef, useEffect } from "react";
import { ArrowUpCircle, ArrowDownCircle } from "lucide-react";
import { formatBytes } from "@/lib/ring-utils";

interface RateWithTrendProps {
  currentRate: number;
  label?: string;
  className?: string;
}

function getRateTrend(current: number, previous: number): "up" | "down" | null {
  if (current === undefined || previous === undefined) {
    return null;
  }

  // Add a small threshold to avoid showing changes for tiny fluctuations
  const threshold = 0.1; // 10% threshold
  const percentChange = Math.abs((current - previous) / previous);

  if (percentChange < threshold) {
    return null;
  }

  return current > previous ? "up" : "down";
}

function TrendIndicator({ trend }: { trend: "up" | "down" | null }) {
  if (!trend) return null;

  return trend === "up" ? (
    <ArrowUpCircle className="inline h-4 w-4 text-green-500 ml-1" />
  ) : (
    <ArrowDownCircle className="inline h-4 w-4 text-red-500 ml-1" />
  );
}

export function RateWithTrend({
  currentRate,
  label,
  className,
}: RateWithTrendProps) {
  const previousRateRef = useRef(currentRate);
  const trend = useMemo(
    () => getRateTrend(currentRate, previousRateRef.current),
    [currentRate]
  );

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      previousRateRef.current = currentRate;
    }, 2000); // Update previous rate after a delay to show trend

    return () => clearTimeout(timeoutId);
  }, [currentRate]);

  return (
    <span className={className}>
      {formatBytes(currentRate)}/s{label && ` ${label}`}
      <TrendIndicator trend={trend} />
    </span>
  );
}
