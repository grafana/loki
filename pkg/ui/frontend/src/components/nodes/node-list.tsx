import React from "react";
import { formatDistanceToNow, parseISO, isValid } from "date-fns";
import { Member } from "@/types/cluster";
import StatusBadge from "@/components/nodes/status-badge";
import { ReadinessIndicator } from "@/components/nodes/readiness-indicator";
import { DataTableColumnHeader } from "@/components/common/data-table-column-header";
import { Button } from "@/components/ui/button";
import { ArrowRightCircle } from "lucide-react";
import { useNavigate } from "react-router-dom";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

type NodeSortField = "name" | "target" | "version" | "buildDate";

interface NodeListProps {
  nodes: { [key: string]: Member };
  sortField: NodeSortField;
  sortDirection: "asc" | "desc";
  onSort: (field: NodeSortField) => void;
}

interface NodeRowProps {
  name: string;
  node: Member;
  onNavigate: (name: string) => void;
}

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

const NodeRow: React.FC<NodeRowProps> = ({ name, node, onNavigate }) => {
  return (
    <TableRow
      key={name}
      className="hover:bg-muted/50 cursor-pointer"
      onClick={() => onNavigate(name)}
    >
      <TableCell className="font-medium">{name}</TableCell>
      <TableCell>{node.target}</TableCell>
      <TableCell className="font-mono text-sm">{node.build.version}</TableCell>
      <TableCell>{formatBuildDate(node.build.buildDate)}</TableCell>
      <TableCell>
        <StatusBadge services={node.services} error={node.error} />
      </TableCell>
      <TableCell>
        <ReadinessIndicator
          isReady={node.ready?.isReady}
          message={node.ready?.message}
        />
      </TableCell>
      <TableCell>
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 p-0"
          onClick={(e) => {
            e.stopPropagation();
            onNavigate(name);
          }}
        >
          <ArrowRightCircle className="h-4 w-4" />
          <span className="sr-only">View details</span>
        </Button>
      </TableCell>
    </TableRow>
  );
};

const NodeList: React.FC<NodeListProps> = ({
  nodes,
  sortField,
  sortDirection,
  onSort,
}) => {
  const navigate = useNavigate();

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

  const handleNavigate = (name: string) => {
    navigate(`/nodes/${name}`);
  };

  return (
    <div className="rounded-md border bg-card">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead className="w-[300px]">
              <DataTableColumnHeader<NodeSortField>
                title="Node Name"
                field="name"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<NodeSortField>
                title="Target"
                field="target"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<NodeSortField>
                title="Version"
                field="version"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[200px]">
              <DataTableColumnHeader<NodeSortField>
                title="Build Date"
                field="buildDate"
                sortField={sortField}
                sortDirection={sortDirection}
                onSort={onSort}
              />
            </TableHead>
            <TableHead className="w-[150px]">Status</TableHead>
            <TableHead className="w-[50px]">Ready</TableHead>
            <TableHead className="w-[100px]">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {sortedNodes.map(([name, node]) => (
            <NodeRow
              key={name}
              name={name}
              node={node}
              onNavigate={handleNavigate}
            />
          ))}
          {sortedNodes.length === 0 && (
            <TableRow>
              <TableCell colSpan={7} className="h-24 text-center">
                <div className="text-muted-foreground">No nodes found</div>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    </div>
  );
};

export default NodeList;
