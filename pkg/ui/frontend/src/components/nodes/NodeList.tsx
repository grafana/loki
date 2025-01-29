import React from "react";
import { formatDistanceToNow, parseISO, isValid } from "date-fns";
import { Member } from "../../types";
import StatusBadge from "./StatusBadge";

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

  const renderSortableHeader = (
    label: string,
    field: "name" | "target" | "version" | "buildDate"
  ) => (
    <th
      className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700"
      onClick={() => onSort(field)}
    >
      {label}
    </th>
  );

  return (
    <div className="bg-white dark:bg-gray-800 shadow rounded-lg overflow-hidden">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            {renderSortableHeader("Node Name", "name")}
            {renderSortableHeader("Target", "target")}
            {renderSortableHeader("Version", "version")}
            {renderSortableHeader("Build Date", "buildDate")}
            <th className="px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
              Status
            </th>
          </tr>
        </thead>
        <tbody className="bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700">
          {sortedNodes.map(([name, node]) => (
            <tr key={name} className="hover:bg-gray-50 dark:hover:bg-gray-700">
              <td className="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-900 dark:text-white">
                {name}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-300">
                {node.target}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-300">
                {node.build.version}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-300">
                {formatBuildDate(node.build.buildDate)}
              </td>
              <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-500 dark:text-gray-300">
                <StatusBadge services={node.services} error={node.error} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};

export default NodeList;
