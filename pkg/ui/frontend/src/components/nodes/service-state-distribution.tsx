import { useMemo } from "react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { NodeState } from "@/types/cluster";

const STATE_COLORS: Record<NodeState, string> = {
  Running: "#10B981", // emerald-500
  Starting: "#F59E0B", // amber-500
  New: "#3B82F6", // blue-500
  Stopping: "#F59E0B", // amber-500
  Terminated: "#6B7280", // gray-500
  Failed: "#EF4444", // red-500
};

interface ServiceStateDistributionProps {
  services: Array<{ service: string; status: string }>;
}

export function ServiceStateDistribution({
  services,
}: ServiceStateDistributionProps) {
  const data = useMemo(() => {
    const stateCounts = services.reduce((acc, { status }) => {
      const state = status as NodeState;
      acc.set(state, (acc.get(state) || 0) + 1);
      return acc;
    }, new Map<NodeState, number>());

    return Array.from(stateCounts.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([state, count]) => ({
        name: state,
        value: count,
        color: STATE_COLORS[state],
      }));
  }, [services]);

  const total = useMemo(() => services.length, [services]);

  if (data.length === 0) {
    return null;
  }

  return (
    <div className="h-[180px] w-full flex items-center">
      <div className="flex-1 relative">
        <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none z-10">
          <div className="text-2xl font-bold">{total}</div>
          <div className="text-xs text-muted-foreground">Services</div>
        </div>
        <ResponsiveContainer width="100%" height={180}>
          <PieChart margin={{ top: 0, right: 0, bottom: 0, left: 0 }}>
            <Pie
              data={data}
              cx="50%"
              cy="50%"
              labelLine={false}
              outerRadius={70}
              innerRadius={50}
              dataKey="value"
              paddingAngle={2}
              strokeWidth={2}
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
      <div className="flex flex-col gap-1.5 min-w-[120px] pl-4">
        {data.map((item) => (
          <div
            key={item.name}
            className="flex items-center justify-between gap-2 text-sm"
          >
            <div className="flex items-center gap-2">
              <div
                className="w-2 h-2 rounded-full shrink-0"
                style={{ backgroundColor: item.color }}
              />
              <span className="text-muted-foreground">{item.name}</span>
            </div>
            <span className="font-medium tabular-nums">{item.value}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
