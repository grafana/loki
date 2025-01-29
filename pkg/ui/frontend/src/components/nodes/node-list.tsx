import React from "react";
import { formatDistanceToNow, parseISO, isValid } from "date-fns";
import { Member } from "@/types/cluster";
import StatusBadge from "@/components/nodes/status-badge";
import { DataTableColumnHeader } from "@/components/nodes/data-table-column-header";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

interface NodeListProps {
  nodes: { [key: string]: Member };
  sortField: "name" | "target" | "version" | "buildDate";
  sortDirection: "asc" | "desc";
  onSort: (field: "name" | "target" | "version" | "buildDate") => void;
}

const NodeList: React.FC<NodeListProps> = ({
  nodes,
  sortField,
  sortDirection,
  onSort,
}) => {
  const formatBuildDate = (dateStr: string) => {
    try {
      const date = parseISO(dateStr);
      if (!isValid(date)) {
        return "Invalid date";
      }
      return formatDistanceToNow(date, { addSuffix: true });
    } catch (error) {
      console.warn("Error parsing date:", dateStr, error);
      return "Invalid date";
    }
  };

  const compareDates = (dateStrA: string, dateStrB: string) => {
    const dateA = parseISO(dateStrA);
    const dateB = parseISO(dateStrB);
    if (!isValid(dateA) && !isValid(dateB)) return 0;
    if (!isValid(dateA)) return 1;
    if (!isValid(dateB)) return -1;
    return dateA.getTime() - dateB.getTime();
  };

  const sortedNodes = Object.entries(nodes).sort(([aKey, a], [bKey, b]) => {
    let comparison = 0;
    switch (sortField) {
      case "name":
        comparison = aKey.localeCompare(bKey);
        break;
      case "target":
        comparison = a.target.localeCompare(b.target);
        break;
      case "version":
        comparison = a.build.version.localeCompare(b.build.version);
        break;
      case "buildDate":
        comparison = compareDates(a.build.buildDate, b.build.buildDate);
        break;
    }
    return sortDirection === "asc" ? comparison : -comparison;
  });

  return (
    <div className="rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>
              <DataTableColumnHeader
                title="Node Name"
                field="name"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead>
              <DataTableColumnHeader
                title="Target"
                field="target"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead>
              <DataTableColumnHeader
                title="Version"
                field="version"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead>
              <DataTableColumnHeader
                title="Build Date"
                field="buildDate"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead>Status</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sortedNodes.map(([name, node]) => (
            <TableRow key={name}>
              <TableCell className="font-medium">{name}</TableCell>
              <TableCell>{node.target}</TableCell>
              <TableCell>{node.build.version}</TableCell>
              <TableCell>{formatBuildDate(node.build.buildDate)}</TableCell>
              <TableCell>
                <StatusBadge services={node.services} error={node.error} />
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
};

export default NodeList;
