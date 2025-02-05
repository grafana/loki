import React from "react";
import { NodeState, ALL_NODE_STATES } from "../../types/cluster";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { MultiSelect } from "@/components/common/multi-select";
import { RefreshCw } from "lucide-react";

interface NodeFiltersProps {
  nameFilter: string;
  targetFilter: string[];
  selectedStates: NodeState[];
  onNameFilterChange: (value: string) => void;
  onTargetFilterChange: (value: string[]) => void;
  onStatesChange: (states: NodeState[]) => void;
  onRefresh: () => void;
  availableTargets: string[];
  isLoading?: boolean;
}

const NodeFilters: React.FC<NodeFiltersProps> = ({
  nameFilter,
  targetFilter,
  selectedStates,
  onNameFilterChange,
  onTargetFilterChange,
  onStatesChange,
  onRefresh,
  availableTargets,
}) => {
  const stateOptions = ALL_NODE_STATES.map((state) => ({
    label: state,
    value: state,
  }));

  const handleStateChange = (values: string[]) => {
    onStatesChange(values as NodeState[]);
  };

  return (
    <div className="grid grid-cols-[auto_1fr_auto] gap-x-4 gap-y-2">
      <div className="space-y-2">
        <div className="space-y-1.5">
          <label className="text-sm font-medium text-muted-foreground">
            Node filters
          </label>
          <Input
            value={nameFilter}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              onNameFilterChange(e.target.value)
            }
            placeholder="Filter by node name..."
            className="w-[300px]"
          />
          <MultiSelect
            options={availableTargets.map((target) => ({
              value: target,
              label: target,
            }))}
            selected={targetFilter}
            onChange={onTargetFilterChange}
            placeholder="All Targets"
            className="w-[300px]"
          />
        </div>
      </div>
      <div className="space-y-1.5 self-end">
        <label className="text-sm font-medium text-muted-foreground">
          Service states
        </label>
        <MultiSelect
          options={stateOptions}
          selected={selectedStates}
          onChange={handleStateChange}
          placeholder="Filter nodes by service states..."
          className="w-full min-w-[300px]"
        />
      </div>
      <div className="self-end">
        <Button
          onClick={onRefresh}
          size="sm"
          variant="outline"
          className="h-9 w-9"
        >
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
};

export default NodeFilters;
