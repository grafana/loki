import { Input } from "@/components/ui/input";
import { MultiSelect, Option } from "@/components/common/multi-select";
import { PartitionInstance, PartitionStates } from "@/types/ring";
import { parseZoneFromOwner } from "@/lib/ring-utils";

interface PartitionRingFiltersProps {
  idFilter: string[];
  onIdFilterChange: (value: string[]) => void;
  stateFilter: string[];
  onStateFilterChange: (value: string[]) => void;
  zoneFilter: string[];
  onZoneFilterChange: (value: string[]) => void;
  ownerFilter: string;
  onOwnerFilterChange: (value: string) => void;
  uniqueStates: string[];
  uniqueZones: string[];
  partitions: PartitionInstance[];
}

export function PartitionRingFilters({
  idFilter,
  onIdFilterChange,
  stateFilter,
  onStateFilterChange,
  zoneFilter,
  onZoneFilterChange,
  ownerFilter,
  onOwnerFilterChange,
  uniqueStates,
  partitions,
}: PartitionRingFiltersProps) {
  // Create options for each filter type
  const stateOptions: Option[] = uniqueStates.map((state) => ({
    value: state,
    label: PartitionStates[parseInt(state) as keyof typeof PartitionStates],
  }));

  // Get unique zones from all owners
  const allZones = new Set<string>();
  partitions.forEach((partition) => {
    partition.owner_ids.forEach((owner) => {
      const zone = parseZoneFromOwner(owner);
      if (zone) allZones.add(zone);
    });
  });

  const zoneOptions: Option[] = Array.from(allZones)
    .sort()
    .map((zone) => ({
      value: zone,
      label: zone,
    }));

  // Get unique partition IDs
  const uniquePartitions = Array.from(
    new Set(partitions.map((p) => p.id.toString()))
  ).sort((a, b) => parseInt(a) - parseInt(b));

  const partitionOptions: Option[] = uniquePartitions.map((id) => ({
    value: id,
    label: `Partition ${id}`,
  }));

  return (
    <div className="flex flex-wrap gap-4">
      <div className="flex-1 min-w-[200px]">
        <Input
          placeholder="Filter by owner name..."
          value={ownerFilter}
          onChange={(e) => onOwnerFilterChange(e.target.value)}
          className="max-w-sm"
        />
      </div>
      <MultiSelect
        options={stateOptions}
        selected={stateFilter}
        onChange={onStateFilterChange}
        placeholder="Filter by state"
        className="w-[200px]"
      />
      <MultiSelect
        options={zoneOptions}
        selected={zoneFilter}
        onChange={onZoneFilterChange}
        placeholder="Filter by zone"
        className="w-[200px]"
      />
      <MultiSelect
        options={partitionOptions}
        selected={idFilter}
        onChange={onIdFilterChange}
        placeholder="Filter by partition ID"
        className="w-[200px]"
      />
    </div>
  );
}
