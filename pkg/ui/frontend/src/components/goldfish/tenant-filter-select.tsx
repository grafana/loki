import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface TenantFilterSelectProps {
  value?: string;
  onChange: (value: string | undefined) => void;
  tenants: string[];
}

export function TenantFilterSelect({ value, onChange, tenants }: TenantFilterSelectProps) {
  const handleClear = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    // Explicitly call onChange with undefined to clear the filter
    onChange(undefined);
  };

  const handleSelectChange = (newValue: string) => {
    // Convert "all" to undefined, otherwise use the selected value
    const actualValue = newValue === "all" ? undefined : newValue;
    onChange(actualValue);
  };

  return (
    <div className="relative flex items-center">
      <Select
        value={value || "all"}
        onValueChange={handleSelectChange}
      >
        <SelectTrigger className="w-[160px] h-8 pr-8">
          <SelectValue placeholder="All Tenants" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">All Tenants</SelectItem>
          {tenants.map((tenant) => (
            <SelectItem key={tenant} value={tenant}>
              {tenant}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      {value && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute right-0 h-8 w-8 p-0 z-10"
          onClick={handleClear}
          type="button"
        >
          <X className="h-3 w-3" />
        </Button>
      )}
    </div>
  );
}