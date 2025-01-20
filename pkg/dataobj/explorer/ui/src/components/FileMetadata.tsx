import React, { useState } from "react";
import { formatBytes } from "../utils/format";

interface CompressionRatioProps {
  compressed: number;
  uncompressed: number;
  showVisualization?: boolean;
}

const CompressionRatio: React.FC<CompressionRatioProps> = ({
  compressed,
  uncompressed,
  showVisualization = false,
}) => {
  if (compressed === 0 || uncompressed === 0) {
    return <span>-</span>;
  }

  const ratio = uncompressed / compressed;
  const hasCompression = ratio > 1;

  return (
    <div className="flex items-center gap-2">
      <div className="font-medium whitespace-nowrap">{ratio.toFixed(1)}x</div>
      {showVisualization && hasCompression && (
        <div className="flex-1 h-2.5 bg-gray-100 border border-gray-200 rounded relative">
          <div
            className="absolute inset-y-0 left-0 bg-blue-600 rounded"
            style={{ width: `${(compressed / uncompressed) * 100}%` }}
          />
        </div>
      )}
    </div>
  );
};

interface PageInfo {
  compressed_size: number;
  uncompressed_size: number;
  rows_count: number;
  values_count: number;
  encoding: string;
  data_offset: number;
  data_size: number;
}

interface ColumnInfo {
  name?: string;
  type: string;
  value_type: string;
  rows_count: number;
  compression: string;
  uncompressed_size: number;
  compressed_size: number;
  metadata_offset: number;
  metadata_size: number;
  values_count: number;
  pages: PageInfo[];
}

interface SectionMetadata {
  type: string;
  totalCompressedSize: number;
  totalUncompressedSize: number;
  columnCount: number;
  columns: ColumnInfo[];
}

interface FileMetadataProps {
  metadata: {
    sections: SectionMetadata[];
    error?: string;
  };
  filename: string;
  onDownload?: () => void;
}

export const FileMetadata: React.FC<FileMetadataProps> = ({
  metadata,
  filename,
  onDownload,
}) => {
  const [expandedSections, setExpandedSections] = useState<
    Record<number, boolean>
  >(
    metadata.sections.reduce((acc, _, index) => ({ ...acc, [index]: true }), {})
  );
  const [expandedColumns, setExpandedColumns] = useState<
    Record<string, boolean>
  >({});

  if (metadata.error) {
    return (
      <div className="p-4 bg-red-100 border border-red-400 text-red-700 rounded">
        Error: {metadata.error}
      </div>
    );
  }

  const toggleSection = (sectionIndex: number) => {
    setExpandedSections((prev) => ({
      ...prev,
      [sectionIndex]: !prev[sectionIndex],
    }));
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
  const streamSection = metadata.sections.find(
    (s) => s.type === "SECTION_TYPE_STREAMS"
  );
  const logSection = metadata.sections.find(
    (s) => s.type === "SECTION_TYPE_LOGS"
  );
  const streamCount = streamSection?.columns[0]?.rows_count;
  const logCount = logSection?.columns[0]?.rows_count;

  return (
    <div className="space-y-6 p-4">
      {/* Thor Dataobj File */}
      <div className="bg-white shadow rounded-lg">
        {/* Overview */}
        <div className="p-4 border-b">
          <div className="flex justify-between items-start mb-4">
            <div>
              <h2 className="text-lg font-semibold mb-2">Thor Dataobj File</h2>
              <p className="text-sm font-mono mb-4">{filename}</p>
            </div>
            {onDownload && (
              <button
                onClick={onDownload}
                className="px-3 py-1 bg-blue-500 text-white rounded hover:bg-blue-600 text-sm"
              >
                Download
              </button>
            )}
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="bg-gray-50 p-3 rounded">
              <div className="text-sm text-gray-500">Compression</div>
              <CompressionRatio
                compressed={totalCompressed}
                uncompressed={totalUncompressed}
                showVisualization
              />
              <div className="text-xs text-gray-500 mt-1">
                {formatBytes(totalCompressed)} →{" "}
                {formatBytes(totalUncompressed)}
              </div>
            </div>
            <div className="bg-gray-50 p-3 rounded">
              <div className="text-sm text-gray-500">Sections</div>
              <div className="font-medium">{metadata.sections.length}</div>
              <div className="text-xs text-gray-500 mt-1">
                {metadata.sections.map((s) => s.type).join(", ")}
              </div>
            </div>
            {streamCount && (
              <div className="bg-gray-50 p-3 rounded">
                <div className="text-sm text-gray-500">Stream Count</div>
                <div className="font-medium">
                  {streamCount.toLocaleString()}
                </div>
              </div>
            )}
            {logCount && (
              <div className="bg-gray-50 p-3 rounded">
                <div className="text-sm text-gray-500">Log Count</div>
                <div className="font-medium">{logCount.toLocaleString()}</div>
              </div>
            )}
          </div>
        </div>

        {/* Sections */}
        <div className="divide-y">
          {metadata.sections.map((section, sectionIndex) => (
            <div key={sectionIndex}>
              {/* Section Header */}
              <div
                className="p-4 cursor-pointer flex justify-between items-center hover:bg-gray-50"
                onClick={() => toggleSection(sectionIndex)}
              >
                <h3 className="text-lg font-semibold">
                  Section: {section.type}
                </h3>
                <svg
                  className={`w-5 h-5 transform transition-transform ${
                    expandedSections[sectionIndex] ? "rotate-180" : ""
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

              {/* Section Content */}
              {expandedSections[sectionIndex] && (
                <div className="p-4 bg-gray-50">
                  {/* Section Stats */}
                  <div className="grid grid-cols-2 md:grid-cols-3 gap-4 mb-6">
                    <div className="bg-gray-50 p-3 rounded">
                      <div className="text-sm text-gray-500">Compression</div>
                      <CompressionRatio
                        compressed={section.totalCompressedSize}
                        uncompressed={section.totalUncompressedSize}
                        showVisualization
                      />
                      <div className="text-xs text-gray-500 mt-1">
                        {formatBytes(section.totalCompressedSize)} →{" "}
                        {formatBytes(section.totalUncompressedSize)}
                      </div>
                    </div>
                    <div className="bg-gray-50 p-3 rounded">
                      <div className="text-sm text-gray-500">Column Count</div>
                      <div className="font-medium">{section.columnCount}</div>
                    </div>
                    <div className="bg-gray-50 p-3 rounded">
                      <div className="text-sm text-gray-500">Type</div>
                      <div className="font-medium">{section.type}</div>
                    </div>
                  </div>

                  {/* Columns */}
                  <div className="space-y-4">
                    <h4 className="font-medium text-lg mb-4">Columns</h4>
                    {section.columns.map((column, columnIndex) => (
                      <div
                        key={columnIndex}
                        className="bg-white shadow rounded-lg overflow-hidden"
                      >
                        {/* Column Header */}
                        <div
                          className="flex justify-between items-center cursor-pointer p-4 border-b hover:bg-gray-50"
                          onClick={() =>
                            toggleColumn(sectionIndex, columnIndex)
                          }
                        >
                          <div>
                            <h5 className="font-medium text-gray-900">
                              {column.name
                                ? `${column.name} (${column.type})`
                                : column.type}
                            </h5>
                            <div className="text-sm text-gray-500">
                              Type: {column.value_type}
                            </div>
                          </div>
                          <div className="flex items-center">
                            <div className="text-sm font-medium text-gray-600 mr-4">
                              Compression: {column.compression}
                            </div>
                            <svg
                              className={`w-4 h-4 transform transition-transform text-gray-400 ${
                                expandedColumns[
                                  `${sectionIndex}-${columnIndex}`
                                ]
                                  ? "rotate-180"
                                  : ""
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
                        </div>

                        {/* Column Content */}
                        {expandedColumns[`${sectionIndex}-${columnIndex}`] && (
                          <div className="p-4 bg-white">
                            {/* Column Stats */}
                            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
                              <div className="bg-gray-50 p-3 rounded-lg">
                                <div className="text-gray-500 mb-1">
                                  Compression ({column.compression})
                                </div>
                                <div className="font-medium whitespace-nowrap">
                                  <CompressionRatio
                                    compressed={column.compressed_size}
                                    uncompressed={column.uncompressed_size}
                                  />
                                </div>
                                <div className="text-xs text-gray-500 mt-1">
                                  {formatBytes(column.compressed_size)} →{" "}
                                  {formatBytes(column.uncompressed_size)}
                                </div>
                              </div>
                              <div className="bg-gray-50 p-3 rounded-lg">
                                <div className="text-gray-500 mb-1">Rows</div>
                                <div className="font-medium">
                                  {column.rows_count.toLocaleString()}
                                </div>
                              </div>
                              <div className="bg-gray-50 p-3 rounded-lg">
                                <div className="text-gray-500 mb-1">
                                  Values Count
                                </div>
                                <div className="font-medium">
                                  {column.values_count.toLocaleString()}
                                </div>
                              </div>
                              <div className="bg-gray-50 p-3 rounded-lg">
                                <div className="text-gray-500 mb-1">Offset</div>
                                <div className="font-medium">
                                  {formatBytes(column.metadata_offset)}
                                </div>
                              </div>
                            </div>

                            {/* Pages */}
                            {column.pages.length > 0 && (
                              <div className="mt-6">
                                <h6 className="font-medium text-sm mb-3">
                                  Pages ({column.pages.length})
                                </h6>
                                <div className="overflow-x-auto rounded-lg border border-gray-200">
                                  <table className="min-w-full text-sm">
                                    <thead>
                                      <tr className="bg-gray-50 border-b border-gray-200">
                                        <th className="text-left p-3 font-medium text-gray-600">
                                          #
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600">
                                          Rows
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600">
                                          Values
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600">
                                          Encoding
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600">
                                          Compression
                                        </th>
                                      </tr>
                                    </thead>
                                    <tbody className="bg-white">
                                      {column.pages.map((page, pageIndex) => (
                                        <tr
                                          key={pageIndex}
                                          className="border-t border-gray-100 hover:bg-gray-50"
                                        >
                                          <td className="p-3">
                                            {pageIndex + 1}
                                          </td>
                                          <td className="p-3">
                                            {page.rows_count.toLocaleString()}
                                          </td>
                                          <td className="p-3">
                                            {page.values_count.toLocaleString()}
                                          </td>
                                          <td className="p-3 font-medium">
                                            {page.encoding}
                                          </td>
                                          <td className="p-3">
                                            <CompressionRatio
                                              compressed={page.compressed_size}
                                              uncompressed={
                                                page.uncompressed_size
                                              }
                                            />
                                          </td>
                                        </tr>
                                      ))}
                                    </tbody>
                                  </table>
                                </div>
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
