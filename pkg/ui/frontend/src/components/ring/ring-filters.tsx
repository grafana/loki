import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { MultiSelect } from "../common/multi-select";

interface RingFiltersProps {
  idFilter: string;
  onIdFilterChange: (value: string) => void;
  stateFilter: string[];
  onStateFilterChange: (value: string[]) => void;
  zoneFilter: string[];
  onZoneFilterChange: (value: string[]) => void;
  uniqueStates: string[];
  uniqueZones: string[];
}

export function RingFilters({
  idFilter,
  onIdFilterChange,
  stateFilter,
  onStateFilterChange,
  zoneFilter,
  onZoneFilterChange,
  uniqueStates,
  uniqueZones,
}: RingFiltersProps) {
  return (
    <div className="flex items-center gap-4">
      <div className="relative flex-1">
        <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Filter by ID..."
          value={idFilter}
          onChange={(e) => onIdFilterChange(e.target.value)}
          className="pl-8"
        />
      </div>
      {uniqueStates.length > 0 && (
        <MultiSelect
          options={uniqueStates.map((state) => ({
            value: state,
            label: state,
          }))}
          selected={stateFilter}
          onChange={onStateFilterChange}
          placeholder="Filter by State"
          className="w-[180px]"
        />
      )}
      {uniqueZones.length > 0 && (
        <MultiSelect
          options={uniqueZones.map((zone) => ({
            value: zone,
            label: zone,
          }))}
          selected={zoneFilter}
          onChange={onZoneFilterChange}
          placeholder="Filter by Zone"
          className="w-[180px]"
        />
      )}
    </div>
  );
}
