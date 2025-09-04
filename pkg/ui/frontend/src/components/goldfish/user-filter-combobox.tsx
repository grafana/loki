import { useState } from "react";
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

  const filteredSuggestions = suggestions.filter(user =>
    user.toLowerCase().includes(inputValue.toLowerCase())
  );

  const handleSelect = (selectedValue: string) => {
    setInputValue(selectedValue);
    onChange(selectedValue);
    setOpen(false);
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const newValue = e.target.value;
    setInputValue(newValue);
    onChange(newValue || undefined);
    // Keep dropdown open when typing
    if (!open && filteredSuggestions.length > 0) {
      setOpen(true);
    }
  };

  const handleClear = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setInputValue("");
    onChange(undefined);
    setOpen(false);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <div className="relative flex items-center">
        <PopoverTrigger asChild>
          <Input
            type="text"
            placeholder="Filter by user..."
            value={inputValue}
            onChange={handleInputChange}
            onClick={() => {
              if (filteredSuggestions.length > 0) {
                setOpen(!open);
              }
            }}
            className="w-[240px] h-8 pr-8"
          />
        </PopoverTrigger>
        {inputValue && (
          <Button
            variant="ghost"
            size="sm"
            className="absolute right-0 h-8 w-8 p-0 z-10"
            onClick={handleClear}
            type="button"
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