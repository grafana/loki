import { ChevronsUpDown, ArrowDown, ArrowUp } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";

interface DataTableColumnHeaderProps<TField extends string> {
  title: string;
  field: TField;
  sortField: string;
  sortDirection: "asc" | "desc";
  onSort: (field: TField) => void;
}

export function DataTableColumnHeader<TField extends string>({
  title,
  field,
  sortField,
  sortDirection,
  onSort,
}: DataTableColumnHeaderProps<TField>) {
  const isCurrentSort = sortField === field;

  const handleSort = (direction: "asc" | "desc") => {
    if (sortField === field && sortDirection === direction) {
      return;
    }
    onSort(field);
  };

  return (
    <div className="flex items-center space-x-2">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="sm"
            className="-ml-3 h-8 hover:bg-muted/50 data-[state=open]:bg-muted/50"
          >
            <div className="flex items-center">
              <span>{title}</span>
              {isCurrentSort ? (
                sortDirection === "desc" ? (
                  <ArrowDown className="ml-2 h-4 w-4" />
                ) : (
                  <ArrowUp className="ml-2 h-4 w-4" />
                )
              ) : (
                <ChevronsUpDown className="ml-2 h-4 w-4" />
              )}
            </div>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem
            onClick={() => handleSort("asc")}
            className={cn(
              "cursor-pointer",
              isCurrentSort && sortDirection === "asc" && "bg-accent"
            )}
          >
            <ArrowUp className="mr-2 h-3.5 w-3.5 text-muted-foreground/70" />
            Asc
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => handleSort("desc")}
            className={cn(
              "cursor-pointer",
              isCurrentSort && sortDirection === "desc" && "bg-accent"
            )}
          >
            <ArrowDown className="mr-2 h-3.5 w-3.5 text-muted-foreground/70" />
            Desc
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
