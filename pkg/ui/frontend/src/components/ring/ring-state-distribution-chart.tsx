import { useMemo } from "react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { RingInstance } from "@/types/ring";

// Map states to their corresponding hex colors
const getStateColor = (state: string): string => {
  switch (state) {
    case "ACTIVE":
      return "#22c55e"; // green-500
    case "LEAVING":
      return "#eab308"; // yellow-500
    case "PENDING":
      return "#3b82f6"; // blue-500
    case "JOINING":
      return "#a855f7"; // purple-500
    case "LEFT":
      return "#ef4444"; // red-500
    default:
      return "#6b7280"; // gray-500
  }
};

interface RingStateDistributionChartProps {
  instances: RingInstance[];
}

export function RingStateDistributionChart({
  instances,
}: RingStateDistributionChartProps) {
  const data = useMemo(() => {
    const stateCounts = new Map<string, number>();

    instances.forEach((instance) => {
      const state = instance.state || "unknown";
      stateCounts.set(state, (stateCounts.get(state) || 0) + 1);
    });

    return Array.from(stateCounts.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([name, value]) => ({
        name,
        value,
        color: getStateColor(name),
      }));
  }, [instances]);

  const totalInstances = useMemo(() => {
    return instances.length;
  }, [instances]);

  if (data.length === 0) {
    return null;
  }

  return (
    <div className="w-full h-[120px] relative">
      <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
        <div className="text-xl font-bold">{totalInstances}</div>
        <div className="text-xs text-muted-foreground">Instances</div>
      </div>
      <ResponsiveContainer width="100%" height="100%">
        <PieChart>
          <Pie
            data={data}
            cx="50%"
            cy="50%"
            labelLine={false}
            outerRadius={60}
            innerRadius={42}
            dataKey="value"
            paddingAngle={1}
            strokeWidth={1}
          >
            {data.map((entry) => (
              <Cell
                key={`cell-${entry.name}`}
                fill={entry.color}
                stroke="hsl(var(--background))"
              />
            ))}
          </Pie>
          <Tooltip
            content={({ active, payload }) => {
              if (!active || !payload || !payload[0]) return null;
              const data = payload[0].payload;
              return (
                <div className="bg-background border rounded-lg shadow-lg px-3 py-2 flex items-center gap-2">
                  <div
                    className="w-2.5 h-2.5 rounded-sm"
                    style={{ backgroundColor: data.color }}
                  />
                  <span className="text-sm font-medium">{data.name}</span>
                  <span className="text-sm font-semibold">{data.value}</span>
                </div>
              );
            }}
          />
        </PieChart>
      </ResponsiveContainer>
    </div>
  );
}
