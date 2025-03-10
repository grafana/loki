import { useMemo } from "react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { PartitionInstance, PartitionStates } from "@/types/ring";

// Map states to their corresponding hex colors
const getStateColor = (state: number): string => {
  switch (state) {
    case 2: // Active
      return "#22c55e"; // green-500
    case 1: // Pending
      return "#3b82f6"; // blue-500
    case 3: // Inactive
      return "#eab308"; // yellow-500
    case 4: // Deleted
      return "#ef4444"; // red-500
    default: // Unknown
      return "#6b7280"; // gray-500
  }
};

interface PartitionStateDistributionChartProps {
  partitions: PartitionInstance[];
}

export function PartitionStateDistributionChart({
  partitions,
}: PartitionStateDistributionChartProps) {
  const data = useMemo(() => {
    const stateCounts = new Map<number, number>();

    partitions.forEach((partition) => {
      const state = partition.state;
      stateCounts.set(state, (stateCounts.get(state) || 0) + 1);
    });

    return Array.from(stateCounts.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([state, value]) => ({
        name: PartitionStates[state as keyof typeof PartitionStates],
        value,
        color: getStateColor(state),
      }));
  }, [partitions]);

  const totalPartitions = useMemo(() => {
    return partitions.length;
  }, [partitions]);

  if (data.length === 0) {
    return null;
  }

  return (
    <div className="w-full h-[120px] relative">
      <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
        <div className="text-xl font-bold">{totalPartitions}</div>
        <div className="text-xs text-muted-foreground">Partitions</div>
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
