import { useState } from "react";
import { Check, ChevronsUpDown, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

interface TenantFilterSelectProps {
  value?: string;
  onChange: (value: string | undefined) => void;
  tenants: string[];
}

export function TenantFilterSelect({ value, onChange, tenants }: TenantFilterSelectProps) {
  const [open, setOpen] = useState(false);
  const [searchValue, setSearchValue] = useState("");

  const handleClear = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    onChange(undefined);
  };

  const handleSelect = (tenant: string | undefined) => {
    onChange(tenant);
    setOpen(false);
    setSearchValue(""); // Reset search when selecting
  };

  // Deduplicate tenants to avoid React key warnings and sort alphabetically
  const uniqueTenants = Array.from(new Set(tenants)).sort();

  return (
    <div className="relative flex items-center">
      <Popover open={open} onOpenChange={(newOpen) => {
        setOpen(newOpen);
        // Reset search when closing
        if (!newOpen) {
          setSearchValue("");
        }
      }}>
        <PopoverTrigger asChild>
          <Button
            variant="outline"
            role="combobox"
            aria-expanded={open}
            className="w-[160px] h-8 pr-8 justify-between"
          >
            {value ?? "All Tenants"}
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[160px] p-0" align="start">
          <Command shouldFilter={false}>
            <CommandInput 
              placeholder="Search tenants..." 
              value={searchValue}
              onValueChange={setSearchValue}
            />
            <CommandList>
              <CommandEmpty>No tenants found</CommandEmpty>
              <CommandGroup>
                {/* All Tenants option - always show if no search or if it matches */}
                {(!searchValue || "all tenants".includes(searchValue.toLowerCase())) && (
                  <CommandItem
                    value="all"
                    onSelect={() => handleSelect(undefined)}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        value === undefined ? "opacity-100" : "opacity-0"
                      )}
                    />
                    All Tenants
                  </CommandItem>
                )}
                {/* Filter tenants based on search */}
                {uniqueTenants
                  .filter(tenant => 
                    !searchValue || tenant.toLowerCase().includes(searchValue.toLowerCase())
                  )
                  .map((tenant) => (
                    <CommandItem
                      key={tenant}
                      value={tenant}
                      onSelect={() => handleSelect(tenant)}
                    >
                      <Check
                        className={cn(
                          "mr-2 h-4 w-4",
                          value === tenant ? "opacity-100" : "opacity-0"
                        )}
                      />
                      {tenant}
                    </CommandItem>
                  ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
      {value && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute right-0 h-8 w-8 p-0 z-10"
          onClick={handleClear}
          type="button"
          aria-label="Clear selection"
        >
          <X className="h-3 w-3" />
        </Button>
      )}
    </div>
  );
}