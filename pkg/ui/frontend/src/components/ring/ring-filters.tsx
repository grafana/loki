import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface RingFiltersProps {
  idFilter: string;
  onIdFilterChange: (value: string) => void;
  stateFilter: string;
  onStateFilterChange: (value: string) => void;
  zoneFilter: string;
  onZoneFilterChange: (value: string) => void;
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
        <Select value={stateFilter} onValueChange={onStateFilterChange}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder="Filter by State" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">All States</SelectItem>
            {uniqueStates.map((state) => (
              <SelectItem key={state} value={state}>
                {state}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {uniqueZones.length > 0 && (
        <Select value={zoneFilter} onValueChange={onZoneFilterChange}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder="Filter by Zone" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">All Zones</SelectItem>
            {uniqueZones.map((zone) => (
              <SelectItem key={zone} value={zone}>
                {zone}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  );
}
