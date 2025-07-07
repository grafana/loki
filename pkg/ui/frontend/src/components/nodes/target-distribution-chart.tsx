import { useMemo } from "react";
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip } from "recharts";
import { Member } from "@/types/cluster";

// Use theme chart colors directly
const getChartColor = (index: number): string => {
  return `hsl(var(--chart-${(index % 6) + 1}))`;
};

interface TargetDistributionChartProps {
  nodes: { [key: string]: Member };
}

export function TargetDistributionChart({
  nodes,
}: TargetDistributionChartProps) {
  const data = useMemo(() => {
    const targetCounts = new Map<string, number>();

    Object.values(nodes).forEach((node) => {
      const target = node.target || "unknown";
      targetCounts.set(target, (targetCounts.get(target) || 0) + 1);
    });

    return Array.from(targetCounts.entries())
      .sort((a, b) => b[1] - a[1])
      .map(([name, value], index) => ({
        name,
        value,
        color: getChartColor(index),
      }));
  }, [nodes]);

  const totalNodes = useMemo(() => {
    return Object.keys(nodes).length;
  }, [nodes]);

  if (data.length === 0) {
    return null;
  }

  return (
    <div className="w-full h-[120px] relative">
      <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
        <div className="text-xl font-bold">{totalNodes}</div>
        <div className="text-xs text-muted-foreground">Nodes</div>
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
            fill="hsl(var(--chart-1))"
            dataKey="value"
            paddingAngle={1}
            strokeWidth={1}
          >
            {data.map((entry, index) => (
              <Cell
                key={`cell-${entry.name}`}
                fill={getChartColor(index)}
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
