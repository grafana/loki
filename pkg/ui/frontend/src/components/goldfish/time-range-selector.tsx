import { useState, useEffect } from 'react';
import { Button } from '@/components/ui/button';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Clock } from 'lucide-react';

interface TimeRangeSelectorProps {
  from: Date | null;
  to: Date | null;
  onChange: (from: Date | null, to: Date | null) => void;
}

type PresetRange = {
  label: string;
  value: string;
  getRange: () => { from: Date; to: Date };
};

const PRESET_RANGES: PresetRange[] = [
  {
    label: 'Last 15 minutes',
    value: '15m',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 15 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 30 minutes',
    value: '30m',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 30 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 1 hour',
    value: '1h',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 60 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 3 hours',
    value: '3h',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 3 * 60 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 6 hours',
    value: '6h',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 6 * 60 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 12 hours',
    value: '12h',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 12 * 60 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 24 hours',
    value: '24h',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 24 * 60 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 2 days',
    value: '2d',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 2 * 24 * 60 * 60 * 1000), to: now };
    },
  },
  {
    label: 'Last 7 days',
    value: '7d',
    getRange: () => {
      const now = new Date();
      return { from: new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000), to: now };
    },
  },
];

export function TimeRangeSelector({ from, to, onChange }: TimeRangeSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const [selectedPreset, setSelectedPreset] = useState<string>('');
  const [customFrom, setCustomFrom] = useState('');
  const [customTo, setCustomTo] = useState('');
  const [isCustom, setIsCustom] = useState(false);
  const [isDefault, setIsDefault] = useState(from === null && to === null);

  // Format date for display
  const formatDate = (date: Date) => {
    return date.toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  // Format date for datetime-local input
  const formatForInput = (date: Date) => {
    const d = new Date(date);
    d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
    return d.toISOString().slice(0, 16);
  };

  // Initialize custom dates when they change
  useEffect(() => {
    if (from && to) {
      setCustomFrom(formatForInput(from));
      setCustomTo(formatForInput(to));
      setIsDefault(false);
    } else {
      // Use current time as default for the inputs
      const now = new Date();
      const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000);
      setCustomFrom(formatForInput(oneHourAgo));
      setCustomTo(formatForInput(now));
      setIsDefault(true);
    }
  }, [from, to]);

  const handlePresetChange = (value: string) => {
    setSelectedPreset(value);
    setIsCustom(false);
    setIsDefault(false);
    const preset = PRESET_RANGES.find(p => p.value === value);
    if (preset) {
      const range = preset.getRange();
      onChange(range.from, range.to);
    }
  };

  const handleCustomApply = () => {
    if (customFrom && customTo) {
      const fromDate = new Date(customFrom);
      const toDate = new Date(customTo);
      if (fromDate < toDate) {
        onChange(fromDate, toDate);
        setIsCustom(true);
        setIsDefault(false);
        setIsOpen(false);
      }
    }
  };

  const handleClear = () => {
    onChange(null, null);
    setSelectedPreset('');
    setIsCustom(false);
    setIsDefault(true);
    setIsOpen(false);
  };

  const getDisplayText = () => {
    if (isDefault) {
      return 'Default (Last 1 hour)';
    }
    if (isCustom && from && to) {
      return `${formatDate(from)} - ${formatDate(to)}`;
    }
    const preset = PRESET_RANGES.find(p => p.value === selectedPreset);
    return preset ? preset.label : 'Custom';
  };

  return (
    <Popover open={isOpen} onOpenChange={setIsOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" className="min-w-[200px] justify-start">
          <Clock className="h-4 w-4 mr-2" />
          {getDisplayText()}
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[320px] p-4" align="end">
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <Label className="text-sm font-medium">
              Time Range
            </Label>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleClear}
              className="h-8 px-2 text-xs"
            >
              Clear to Default
            </Button>
          </div>
          
          <div>
            <Label htmlFor="preset" className="text-sm font-medium mb-2 block">
              Quick ranges
            </Label>
            <Select value={selectedPreset} onValueChange={handlePresetChange}>
              <SelectTrigger id="preset">
                <SelectValue placeholder="Select a preset" />
              </SelectTrigger>
              <SelectContent>
                {PRESET_RANGES.map(preset => (
                  <SelectItem key={preset.value} value={preset.value}>
                    {preset.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="border-t pt-4">
            <Label className="text-sm font-medium mb-2 block">
              Custom range
            </Label>
            <div className="space-y-2">
              <div>
                <Label htmlFor="from" className="text-xs text-muted-foreground">From</Label>
                <Input
                  id="from"
                  type="datetime-local"
                  value={customFrom}
                  onChange={(e) => setCustomFrom(e.target.value)}
                  className="w-full"
                />
              </div>
              <div>
                <Label htmlFor="to" className="text-xs text-muted-foreground">To</Label>
                <Input
                  id="to"
                  type="datetime-local"
                  value={customTo}
                  onChange={(e) => setCustomTo(e.target.value)}
                  className="w-full"
                />
              </div>
              <Button 
                onClick={handleCustomApply} 
                className="w-full"
                disabled={!customFrom || !customTo || new Date(customFrom) >= new Date(customTo)}
              >
                Apply Custom Range
              </Button>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}