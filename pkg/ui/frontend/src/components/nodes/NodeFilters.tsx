import React from "react";
import { NodeState } from "../../types";

interface NodeFiltersProps {
  nameFilter: string;
  targetFilter: string;
  selectedStates: NodeState[];
  onNameFilterChange: (value: string) => void;
  onTargetFilterChange: (value: string) => void;
  onStatesChange: (states: NodeState[]) => void;
  onRefresh: () => void;
  availableTargets: string[];
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
  const allStates: NodeState[] = [
    "New",
    "Starting",
    "Running",
    "Stopping",
    "Terminated",
    "Failed",
  ];

  const handleStateToggle = (state: NodeState) => {
    if (selectedStates.includes(state)) {
      onStatesChange(selectedStates.filter((s) => s !== state));
    } else {
      onStatesChange([...selectedStates, state]);
    }
  };

  const handleSelectAll = () => {
    if (selectedStates.length === allStates.length) {
      onStatesChange([]);
    } else {
      onStatesChange([...allStates]);
    }
  };

  return (
    <div className="bg-white dark:bg-gray-800 p-4 rounded-lg shadow mb-6 space-y-4">
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <div>
          <label
            htmlFor="name-filter"
            className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
          >
            Node Name
          </label>
          <input
            id="name-filter"
            type="text"
            value={nameFilter}
            onChange={(e) => onNameFilterChange(e.target.value)}
            placeholder="Filter by name..."
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm focus:ring-indigo-500 focus:border-indigo-500 dark:bg-gray-700 dark:text-white"
          />
        </div>

        <div>
          <label
            htmlFor="target-filter"
            className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1"
          >
            Target
          </label>
          <select
            id="target-filter"
            value={targetFilter}
            onChange={(e) => onTargetFilterChange(e.target.value)}
            className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm focus:ring-indigo-500 focus:border-indigo-500 dark:bg-gray-700 dark:text-white"
          >
            <option value="">All Targets</option>
            {availableTargets.map((target) => (
              <option key={target} value={target}>
                {target}
              </option>
            ))}
          </select>
        </div>

        <div className="col-span-2">
          <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
            Status
          </label>
          <div className="bg-white dark:bg-gray-700 border border-gray-300 dark:border-gray-600 rounded-md p-2 space-y-2">
            <div className="flex items-center">
              <input
                type="checkbox"
                id="select-all"
                checked={selectedStates.length === allStates.length}
                onChange={handleSelectAll}
                className="h-4 w-4 text-indigo-600 focus:ring-indigo-500 border-gray-300 rounded"
              />
              <label
                htmlFor="select-all"
                className="ml-2 text-sm text-gray-700 dark:text-gray-300"
              >
                Select All
              </label>
            </div>
            <div className="grid grid-cols-2 gap-2">
              {allStates.map((state) => (
                <div key={state} className="flex items-center">
                  <input
                    type="checkbox"
                    id={`state-${state}`}
                    checked={selectedStates.includes(state)}
                    onChange={() => handleStateToggle(state)}
                    className="h-4 w-4 text-indigo-600 focus:ring-indigo-500 border-gray-300 rounded"
                  />
                  <label
                    htmlFor={`state-${state}`}
                    className="ml-2 text-sm text-gray-700 dark:text-gray-300"
                  >
                    {state}
                  </label>
                </div>
              ))}
            </div>
          </div>
        </div>

        <div className="md:col-span-4 flex justify-end">
          <button
            onClick={onRefresh}
            className="px-4 py-2 bg-indigo-600 text-white rounded-md hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500 dark:bg-indigo-500 dark:hover:bg-indigo-600"
          >
            Refresh
          </button>
        </div>
      </div>
    </div>
  );
};

export default NodeFilters;
