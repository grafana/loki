import React from "react";
import {
  NodeState,
  ALL_NODE_STATES,
  ALL_VALUES_TARGET,
} from "../../types/cluster";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { MultiSelect } from "@/components/common/multi-select";
import { RefreshCw } from "lucide-react";

interface NodeFiltersProps {
  nameFilter: string;
  targetFilter: string;
  selectedStates: NodeState[];
  onNameFilterChange: (value: string) => void;
  onTargetFilterChange: (value: string) => void;
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
    <div className="flex items-center gap-2">
      <Input
        className="w-[225px]"
        value={nameFilter}
        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
          onNameFilterChange(e.target.value)
        }
        placeholder="Filter by node name..."
      />
      <Select value={targetFilter} onValueChange={onTargetFilterChange}>
        <SelectTrigger className="w-[200px]">
          <SelectValue placeholder="All Targets" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem key={ALL_VALUES_TARGET} value={ALL_VALUES_TARGET}>
            All Targets
          </SelectItem>
          {availableTargets.map((target) => (
            <SelectItem key={target} value={target}>
              {target}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <div className="flex-1">
        <MultiSelect
          options={stateOptions}
          defaultValue={selectedStates}
          onValueChange={handleStateChange}
          placeholder="Select states..."
        />
      </div>
      <Button
        onClick={onRefresh}
        size="icon"
        variant="outline"
        className="h-9 w-9 shrink-0 bg-background"
      >
        <RefreshCw className="h-4 w-4" />
      </Button>
    </div>
  );
};

export default NodeFilters;
