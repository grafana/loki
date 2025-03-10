import { Link } from "react-router-dom";
import { DownloadIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { formatBytes } from "@/lib/utils";
import { FileMetadataResponse } from "@/types/explorer";
import { DateHover } from "@/components/common/date-hover";
import { CopyButton } from "../common/copy-button";
import { CompressionRatio } from "../common/compression-ratio";
import { useState } from "react";

// Value type to badge styling mapping
const getValueTypeBadgeStyle = (valueType: string): string => {
  switch (valueType) {
    case "INT64":
      return "bg-blue-500/20 text-blue-700 dark:bg-blue-500/30 dark:text-blue-300 hover:bg-blue-500/30";
    case "BYTES":
      return "bg-red-500/20 text-red-700 dark:bg-red-500/30 dark:text-red-300 hover:bg-red-500/30";
    case "FLOAT64":
      return "bg-purple-500/20 text-purple-700 dark:bg-purple-500/30 dark:text-purple-300 hover:bg-purple-500/30";
    case "BOOL":
      return "bg-yellow-500/20 text-yellow-700 dark:bg-yellow-500/30 dark:text-yellow-300 hover:bg-yellow-500/30";
    case "STRING":
      return "bg-green-500/20 text-green-700 dark:bg-green-500/30 dark:text-green-300 hover:bg-green-500/30";
    case "TIMESTAMP":
      return "bg-orange-500/20 text-orange-700 dark:bg-orange-500/30 dark:text-orange-300 hover:bg-orange-500/30";
    default:
      return "bg-gray-500/20 text-gray-700 dark:bg-gray-500/30 dark:text-gray-300 hover:bg-gray-500/30";
  }
};

interface FileMetadataViewProps {
  metadata: FileMetadataResponse;
  filename: string;
  downloadUrl: string;
}

export function FileMetadataView({
  metadata,
  filename,
  downloadUrl,
}: FileMetadataViewProps) {
  const [expandedSectionIndex, setExpandedSectionIndex] = useState<
    number | null
  >(null);
  const [expandedColumns, setExpandedColumns] = useState<
    Record<string, boolean>
  >({});

  const toggleSection = (sectionIndex: number) => {
    setExpandedSectionIndex(
      expandedSectionIndex === sectionIndex ? null : sectionIndex
    );
  };

  const toggleColumn = (sectionIndex: number, columnIndex: number) => {
    const key = `${sectionIndex}-${columnIndex}`;
    setExpandedColumns((prev) => ({
      ...prev,
      [key]: !prev[key],
    }));
  };

  // Calculate file-level stats
  const totalCompressed = metadata.sections.reduce(
    (sum, section) => sum + section.totalCompressedSize,
    0
  );
  const totalUncompressed = metadata.sections.reduce(
    (sum, section) => sum + section.totalUncompressedSize,
    0
  );

  // Get stream and log counts from first column of each section
  const streamSection = metadata.sections.filter(
    (s) => s.type === "SECTION_TYPE_STREAMS"
  );
  const logSection = metadata.sections.filter(
    (s) => s.type === "SECTION_TYPE_LOGS"
  );
  const streamCount = streamSection?.reduce(
    (sum, sec) => sum + (sec.columns[0].rows_count || 0),
    0
  );
  const logCount = logSection?.reduce(
    (sum, sec) => sum + (sec.columns[0].rows_count || 0),
    0
  );

  return (
    <Card className="w-full">
      <FileHeader
        filename={filename}
        downloadUrl={downloadUrl}
        lastModified={metadata.lastModified}
      />
      <CardContent className="space-y-8">
        <HeadlineStats
          totalCompressed={totalCompressed}
          totalUncompressed={totalUncompressed}
          sections={metadata.sections}
          streamCount={streamCount}
          logCount={logCount}
        />
        <SectionsList
          sections={metadata.sections}
          expandedSectionIndex={expandedSectionIndex}
          expandedColumns={expandedColumns}
          onToggleSection={toggleSection}
          onToggleColumn={toggleColumn}
        />
      </CardContent>
    </Card>
  );
}

// Sub-components

interface FileHeaderProps {
  filename: string;
  downloadUrl: string;
  lastModified: string;
}

function FileHeader({ filename, downloadUrl, lastModified }: FileHeaderProps) {
  return (
    <CardHeader className="space-y-4">
      <div className="flex items-center justify-between">
        <CardTitle className="text-2xl font-semibold tracking-tight">
          Thor Dataobj File
        </CardTitle>
        <Button asChild variant="outline">
          <Link to={downloadUrl} target="_blank" download>
            <DownloadIcon className="h-4 w-4 mr-2" />
            Download
          </Link>
        </Button>
      </div>
      <CardDescription className="space-y-2">
        <div className="flex items-center justify-between">
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <span className="font-mono text-sm text-foreground">
                {filename}
              </span>
              <CopyButton text={filename} />
            </div>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <span>Last Modified:</span>
              <DateHover date={new Date(lastModified)} />
            </div>
          </div>
        </div>
      </CardDescription>
    </CardHeader>
  );
}

interface HeadlineStatsProps {
  totalCompressed: number;
  totalUncompressed: number;
  sections: FileMetadataResponse["sections"];
  streamCount?: number;
  logCount?: number;
}

function HeadlineStats({
  totalCompressed,
  totalUncompressed,
  sections,
  streamCount,
  logCount,
}: HeadlineStatsProps) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
      <div className="rounded-lg bg-muted/50 p-6 shadow-sm">
        <div className="text-sm text-muted-foreground mb-2">Compression</div>
        <CompressionRatio
          compressed={totalCompressed}
          uncompressed={totalUncompressed}
          showVisualization
        />
        <div className="text-xs text-muted-foreground mt-2">
          {formatBytes(totalCompressed)} → {formatBytes(totalUncompressed)}
        </div>
      </div>
      <div className="rounded-lg bg-muted/50 p-6 shadow-sm">
        <div className="text-sm text-muted-foreground mb-2">Sections</div>
        <div className="font-medium text-lg">{sections.length}</div>
        <div className="text-xs text-muted-foreground mt-2">
          {sections.map((s) => s.type).join(", ")}
        </div>
      </div>
      {streamCount && (
        <div className="rounded-lg bg-muted/50 p-6 shadow-sm">
          <div className="text-sm text-muted-foreground mb-2">Stream Count</div>
          <div className="font-medium text-lg">
            {streamCount.toLocaleString()}
          </div>
        </div>
      )}
      {logCount && (
        <div className="rounded-lg bg-muted/50 p-6 shadow-sm">
          <div className="text-sm text-muted-foreground mb-2">Log Count</div>
          <div className="font-medium text-lg">{logCount.toLocaleString()}</div>
        </div>
      )}
    </div>
  );
}

interface SectionsListProps {
  sections: FileMetadataResponse["sections"];
  expandedSectionIndex: number | null;
  expandedColumns: Record<string, boolean>;
  onToggleSection: (index: number) => void;
  onToggleColumn: (sectionIndex: number, columnIndex: number) => void;
}

function SectionsList({
  sections,
  expandedSectionIndex,
  expandedColumns,
  onToggleSection,
  onToggleColumn,
}: SectionsListProps) {
  return (
    <div className="divide-y divide-border">
      {sections.map((section, sectionIndex) => (
        <Section
          key={sectionIndex}
          section={section}
          sectionIndex={sectionIndex}
          isExpanded={expandedSectionIndex === sectionIndex}
          expandedColumns={expandedColumns}
          onToggle={() => onToggleSection(sectionIndex)}
          onToggleColumn={(columnIndex) =>
            onToggleColumn(sectionIndex, columnIndex)
          }
        />
      ))}
    </div>
  );
}

interface SectionProps {
  section: FileMetadataResponse["sections"][0];
  sectionIndex: number;
  isExpanded: boolean;
  expandedColumns: Record<string, boolean>;
  onToggle: () => void;
  onToggleColumn: (columnIndex: number) => void;
}

function Section({
  section,
  sectionIndex,
  isExpanded,
  expandedColumns,
  onToggle,
  onToggleColumn,
}: SectionProps) {
  return (
    <div className="py-4">
      <button
        className="w-full flex justify-between items-center py-4 px-6 rounded-lg hover:bg-accent/50 transition-colors"
        onClick={onToggle}
      >
        <h3 className="text-lg font-semibold">
          Section #{sectionIndex + 1}: {section.type}
        </h3>
        <svg
          className={`w-5 h-5 transform transition-transform duration-300 ${isExpanded ? "rotate-180" : ""
            }`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth="2"
            d="M19 9l-7 7-7-7"
          />
        </svg>
      </button>

      {isExpanded && (
        <div className="mt-6 px-6">
          <SectionStats section={section} />
          <ColumnsList
            columns={section.columns}
            sectionIndex={sectionIndex}
            expandedColumns={expandedColumns}
            onToggleColumn={onToggleColumn}
          />
        </div>
      )}
    </div>
  );
}

interface SectionStatsProps {
  section: FileMetadataResponse["sections"][0];
}

function SectionStats({ section }: SectionStatsProps) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-8">
      <div className="rounded-lg bg-secondary/50 p-6 shadow-sm">
        <div className="text-sm text-muted-foreground mb-2">Compression</div>
        <CompressionRatio
          compressed={section.totalCompressedSize}
          uncompressed={section.totalUncompressedSize}
          showVisualization
        />
        <div className="text-xs text-muted-foreground mt-2">
          {formatBytes(section.totalCompressedSize)} →{" "}
          {formatBytes(section.totalUncompressedSize)}
        </div>
      </div>
      <div className="rounded-lg bg-secondary/50 p-6 shadow-sm">
        <div className="text-sm text-muted-foreground mb-2">Column Count</div>
        <div className="font-medium text-lg">{section.columnCount}</div>
      </div>
      <div className="rounded-lg bg-secondary/50 p-6 shadow-sm">
        <div className="text-sm text-muted-foreground mb-2">Type</div>
        <div className="font-medium text-lg flex items-center gap-2">
          <Badge variant="outline" className="font-mono">
            {section.type}
          </Badge>
        </div>
      </div>
    </div>
  );
}

interface ColumnsListProps {
  columns: FileMetadataResponse["sections"][0]["columns"];
  sectionIndex: number;
  expandedColumns: Record<string, boolean>;
  onToggleColumn: (columnIndex: number) => void;
}

function ColumnsList({
  columns,
  sectionIndex,
  expandedColumns,
  onToggleColumn,
}: ColumnsListProps) {
  return (
    <div className="space-y-6">
      <h4 className="text-lg font-medium">Columns ({columns.length})</h4>
      <div className="space-y-4">
        {columns.map((column, columnIndex) => (
          <Column
            key={columnIndex}
            column={column}
            isExpanded={expandedColumns[`${sectionIndex}-${columnIndex}`]}
            onToggle={() => onToggleColumn(columnIndex)}
          />
        ))}
      </div>
    </div>
  );
}

interface ColumnProps {
  column: FileMetadataResponse["sections"][0]["columns"][0];
  isExpanded: boolean;
  onToggle: () => void;
}

function Column({ column, isExpanded, onToggle }: ColumnProps) {
  return (
    <Card className="bg-card/50">
      <button
        className="w-full flex justify-between items-center p-6 hover:bg-accent/50 transition-colors rounded-t-lg"
        onClick={onToggle}
      >
        <div>
          <h5 className="font-medium text-lg">
            {column.name ? `${column.name} (${column.type})` : column.type}
          </h5>
          <div className="text-sm text-muted-foreground mt-1 flex items-center gap-2">
            <Badge
              variant="secondary"
              className={cn(
                "font-mono text-xs",
                getValueTypeBadgeStyle(column.value_type)
              )}
            >
              {column.value_type}
            </Badge>
          </div>
        </div>
        <div className="flex items-center gap-4">
          <div className="text-sm font-medium flex items-center gap-2">
            Compression:
            <Badge variant="outline" className="font-mono">
              {column.compression || "NONE"}
            </Badge>
          </div>
          <svg
            className={`w-4 h-4 transform transition-transform ${isExpanded ? "rotate-180" : ""
              }`}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth="2"
              d="M19 9l-7 7-7-7"
            />
          </svg>
        </div>
      </button>

      {isExpanded && (
        <CardContent className="pt-6">
          <ColumnStats column={column} />
          {column.pages.length > 0 && <ColumnPages pages={column.pages} />}
        </CardContent>
      )}
    </Card>
  );
}

interface ColumnStatsProps {
  column: FileMetadataResponse["sections"][0]["columns"][0];
}

function ColumnStats({ column }: ColumnStatsProps) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-4 gap-6 mb-8">
      <div className="rounded-lg bg-muted p-6">
        <div className="text-sm text-muted-foreground mb-2 flex items-center gap-2">
          <Badge variant="outline" className="font-mono">
            {column.compression || "NONE"}
          </Badge>
        </div>
        <div className="font-medium">
          <CompressionRatio
            compressed={column.compressed_size}
            uncompressed={column.uncompressed_size}
          />
        </div>
        <div className="text-xs text-muted-foreground mt-2">
          {formatBytes(column.compressed_size)} →{" "}
          {formatBytes(column.uncompressed_size)}
        </div>
      </div>
      <div className="rounded-lg bg-muted p-6">
        <div className="text-sm text-muted-foreground mb-2">Rows</div>
        <div className="font-medium text-lg">
          {column.rows_count.toLocaleString()}
        </div>
      </div>
      <div className="rounded-lg bg-muted p-6">
        <div className="text-sm text-muted-foreground mb-2">Values Count</div>
        <div className="font-medium text-lg">
          {column.values_count.toLocaleString()}
        </div>
      </div>
      <div className="rounded-lg bg-muted p-6">
        <div className="text-sm text-muted-foreground mb-2">Offset</div>
        <div className="font-medium text-lg">
          {formatBytes(column.metadata_offset)}
        </div>
      </div>
      {column.statistics && (
        <div className="col-span-full">
          <div className="rounded-lg bg-muted p-6">
            <div className="text-sm text-muted-foreground mb-4">Statistics</div>
            <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4">
              {column.statistics.cardinality_count !== undefined && (
                <div>
                  <div className="text-sm text-muted-foreground">Cardinality</div>
                  <div className="font-medium">
                    {column.statistics.cardinality_count.toLocaleString()}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

interface ColumnPagesProps {
  pages: FileMetadataResponse["sections"][0]["columns"][0]["pages"];
}

function ColumnPages({ pages }: ColumnPagesProps) {
  return (
    <div className="mt-8">
      <h6 className="text-base font-medium mb-4">Pages ({pages.length})</h6>
      <div className="rounded-lg border border-border overflow-hidden bg-background">
        <table className="w-full">
          <thead>
            <tr className="bg-secondary/50 border-b border-border">
              <th className="text-left p-4 font-medium text-muted-foreground">
                #
              </th>
              <th className="text-left p-4 font-medium text-muted-foreground">
                Rows
              </th>
              <th className="text-left p-4 font-medium text-muted-foreground">
                Values
              </th>
              <th className="text-left p-4 font-medium text-muted-foreground">
                Encoding
              </th>
              <th className="text-left p-4 font-medium text-muted-foreground">
                Compression
              </th>
            </tr>
          </thead>
          <tbody>
            {pages.map((page, pageIndex) => (
              <tr
                key={pageIndex}
                className="border-t border-border hover:bg-accent/50 transition-colors"
              >
                <td className="p-4">{pageIndex + 1}</td>
                <td className="p-4">{page.rows_count.toLocaleString()}</td>
                <td className="p-4">{page.values_count.toLocaleString()}</td>
                <td className="p-4">
                  <Badge variant="outline" className="font-mono">
                    {page.encoding}
                  </Badge>
                </td>
                <td className="p-4">
                  <div className="flex items-center gap-2">
                    <CompressionRatio
                      compressed={page.compressed_size}
                      uncompressed={page.uncompressed_size}
                    />
                    <span className="text-xs text-muted-foreground">
                      ({formatBytes(page.compressed_size)} →{" "}
                      {formatBytes(page.uncompressed_size)})
                    </span>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
