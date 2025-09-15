import { useState, useEffect, useRef } from "react";
import { Check, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";

interface UserFilterComboboxProps {
  value?: string;
  onChange: (value: string | undefined) => void;
  suggestions: string[];
}

export function UserFilterCombobox({ value, onChange, suggestions }: UserFilterComboboxProps) {
  const [open, setOpen] = useState(false);
  const [inputValue, setInputValue] = useState(value || "");
  const [searchTerm, setSearchTerm] = useState(value || ""); // Used for filtering
  const debounceTimer = useRef<NodeJS.Timeout | null>(null);

  // Update state when value prop changes
  useEffect(() => {
    setInputValue(value || "");
    setSearchTerm(value || "");
  }, [value]);

  // Deduplicate suggestions first, then filter based on searchTerm (not inputValue)
  const uniqueSuggestions = Array.from(new Set(suggestions));
  const filteredSuggestions = uniqueSuggestions.filter(user =>
    user.toLowerCase().includes(searchTerm.toLowerCase())
  );

  const handleSelect = (selectedValue: string) => {
    setInputValue(selectedValue);
    setSearchTerm(selectedValue); // Update search term too
    onChange(selectedValue);
    setOpen(false);
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.value;
    setInputValue(newValue);
    
    // Clear any existing debounce timer
    if (debounceTimer.current) {
      clearTimeout(debounceTimer.current);
    }
    
    // Debounce the search term update (1s delay)
    debounceTimer.current = setTimeout(() => {
      setSearchTerm(newValue);
      onChange(newValue || undefined);
      
      // Check if we should show dropdown after debounce
      const newFilteredSuggestions = uniqueSuggestions.filter(user =>
        user.toLowerCase().includes(newValue.toLowerCase())
      );
      
      if (newFilteredSuggestions.length > 0) {
        setOpen(true);
      }
    }, 1000);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      // Clear debounce timer and apply immediately
      if (debounceTimer.current) {
        clearTimeout(debounceTimer.current);
      }
      setSearchTerm(inputValue);
      onChange(inputValue || undefined);
    }
  };

  const handleClear = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (debounceTimer.current) {
      clearTimeout(debounceTimer.current);
    }
    setInputValue("");
    setSearchTerm("");
    onChange(undefined);
    setOpen(false);
  };

  // Cleanup timer on unmount
  useEffect(() => {
    return () => {
      if (debounceTimer.current) {
        clearTimeout(debounceTimer.current);
      }
    };
  }, []);

  const handleFocus = () => {
    if (filteredSuggestions.length > 0) {
      setOpen(true);
    }
  };

  return (
    <Popover open={open} onOpenChange={() => {}}>
      <div className="relative flex items-center">
        <PopoverTrigger asChild>
          <Input
            type="text"
            placeholder="Filter by user..."
            value={inputValue}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            onFocus={handleFocus}
            onBlur={() => {
              // Small delay to allow clicking on suggestions
              setTimeout(() => {
                setOpen(false);
              }, 200);
            }}
            className="w-[240px] h-8 pr-8"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-haspopup="listbox"
          />
        </PopoverTrigger>
        {inputValue && (
          <Button
            variant="ghost"
            size="sm"
            className="absolute right-0 h-8 w-8 p-0 z-10"
            onClick={handleClear}
            type="button"
            aria-label="Clear selection"
          >
            <X className="h-4 w-4" />
          </Button>
        )}
      </div>
      {open && filteredSuggestions.length > 0 && (
        <PopoverContent 
          className="w-[240px] p-0" 
          align="start"
          onOpenAutoFocus={(e) => e.preventDefault()}
          sideOffset={4}
        >
          <Command>
            <CommandList>
              <CommandEmpty>No users found.</CommandEmpty>
              <CommandGroup>
                {filteredSuggestions.map((user) => (
                  <CommandItem
                    key={user}
                    value={user}
                    onSelect={() => handleSelect(user)}
                  >
                    <Check
                      className={cn(
                        "mr-2 h-4 w-4",
                        value === user ? "opacity-100" : "opacity-0"
                      )}
                    />
                    {user}
                  </CommandItem>
                ))}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      )}
    </Popover>
  );
}
