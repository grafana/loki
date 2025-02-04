import React, { useState } from "react";
import { formatBytes } from "../../utils/format";
import { DateWithHover } from "../common/DateWithHover";
import { Link } from "react-router-dom";
import { useBasename } from "../../contexts/BasenameContext";
import { CompressionRatio } from "./CompressionRatio";
import { FileMetadataResponse } from "../../types/metadata";

interface FileMetadataProps {
  metadata: FileMetadataResponse;
  filename: string;
  className?: string;
}

export const FileMetadata: React.FC<FileMetadataProps> = ({
  metadata,
  filename,
  className = "",
}) => {
  const [expandedSectionIndex, setExpandedSectionIndex] = useState<
    number | null
  >(null);
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

  const basename = useBasename();

  return (
    <div className={`space-y-6 p-4 ${className}`}>
      {/* Thor Dataobj File */}
      <div className="bg-white dark:bg-gray-700 shadow rounded-lg">
        {/* Overview */}
        <div className="p-4 border-b dark:border-gray-700">
          <div className="flex justify-between items-start mb-4">
            <div>
              <h2 className="text-lg font-semibold mb-2 dark:text-gray-200">
                Thor Dataobj File
              </h2>
              <div className="flex flex-col gap-1">
                <p className="text-sm font-mono dark:text-gray-300">
                  {filename}
                </p>
                {metadata.lastModified && (
                  <div className="text-sm text-gray-500 dark:text-gray-400 flex items-center gap-2">
                    <span>Last modified:</span>
                    <DateWithHover date={new Date(metadata.lastModified)} />
                  </div>
                )}
              </div>
            </div>
            <Link
              to={`/api/download?file=${encodeURIComponent(filename)}`}
              target="_blank"
              rel="noopener noreferrer"
              className="px-3 py-1 bg-blue-500 text-white rounded hover:bg-blue-600 text-sm"
            >
              Download
            </Link>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            <div className="bg-gray-50 dark:bg-gray-800 p-3 rounded">
              <div className="text-sm text-gray-500 dark:text-gray-400">
                Compression
              </div>
              <CompressionRatio
                compressed={totalCompressed}
                uncompressed={totalUncompressed}
                showVisualization
              />
              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {formatBytes(totalCompressed)} →{" "}
                {formatBytes(totalUncompressed)}
              </div>
            </div>
            <div className="bg-gray-50 dark:bg-gray-800 p-3 rounded">
              <div className="text-sm text-gray-500 dark:text-gray-400">
                Sections
              </div>
              <div className="font-medium">{metadata.sections.length}</div>
              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {metadata.sections.map((s) => s.type).join(", ")}
              </div>
            </div>
            {streamCount && (
              <div className="bg-gray-50 dark:bg-gray-800 p-3 rounded">
                <div className="text-sm text-gray-500 dark:text-gray-400">
                  Stream Count
                </div>
                <div className="font-medium">
                  {streamCount.toLocaleString()}
                </div>
              </div>
            )}
            {logCount && (
              <div className="bg-gray-50 dark:bg-gray-800 p-3 rounded">
                <div className="text-sm text-gray-500 dark:text-gray-400">
                  Log Count
                </div>
                <div className="font-medium">{logCount.toLocaleString()}</div>
              </div>
            )}
          </div>
        </div>

        {/* Sections */}
        <div className="divide-y dark:divide-gray-900">
          {metadata.sections.map((section, sectionIndex) => (
            <div key={sectionIndex} className="dark:bg-gray-700">
              {/* Section Header */}
              <div
                className="p-4 cursor-pointer flex justify-between items-center hover:bg-gray-50 dark:hover:bg-gray-700"
                onClick={() => toggleSection(sectionIndex)}
              >
                <h3 className="text-lg font-semibold dark:text-gray-200">
                  Section #{sectionIndex + 1}: {section.type}
                </h3>
                <svg
                  className={`w-5 h-5 transform transition-transform duration-700 ${
                    expandedSectionIndex === sectionIndex ? "rotate-180" : ""
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
              <div
                className={`transition-all duration-700 ease-in-out ${
                  expandedSectionIndex === sectionIndex
                    ? "opacity-100"
                    : "opacity-0 hidden"
                }`}
              >
                <div className="p-4 bg-gray-50 dark:bg-gray-800">
                  {/* Section Stats */}
                  <div className="grid grid-cols-2 md:grid-cols-3 gap-4 mb-6">
                    <div className="bg-white dark:bg-gray-700 p-3 rounded">
                      <div className="text-sm text-gray-500 dark:text-gray-400">
                        Compression
                      </div>
                      <CompressionRatio
                        compressed={section.totalCompressedSize}
                        uncompressed={section.totalUncompressedSize}
                        showVisualization
                      />
                      <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                        {formatBytes(section.totalCompressedSize)} →{" "}
                        {formatBytes(section.totalUncompressedSize)}
                      </div>
                    </div>
                    <div className="bg-white dark:bg-gray-700 p-3 rounded">
                      <div className="text-sm text-gray-500 dark:text-gray-400">
                        Column Count
                      </div>
                      <div className="font-medium">{section.columnCount}</div>
                    </div>
                    <div className="bg-white dark:bg-gray-700 p-3 rounded">
                      <div className="text-sm text-gray-500 dark:text-gray-400">
                        Type
                      </div>
                      <div className="font-medium">{section.type}</div>
                    </div>
                  </div>

                  {/* Columns */}
                  <div className="space-y-4">
                    <h4 className="font-medium text-lg mb-4 dark:text-gray-200">
                      Columns ({section.columnCount})
                    </h4>
                    {section.columns.map((column, columnIndex) => (
                      <div
                        key={columnIndex}
                        className="bg-white dark:bg-gray-700 shadow rounded-lg overflow-hidden"
                      >
                        {/* Column Header */}
                        <div
                          className="flex justify-between items-center cursor-pointer p-4 border-b dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600"
                          onClick={() =>
                            toggleColumn(sectionIndex, columnIndex)
                          }
                        >
                          <div>
                            <h5 className="font-medium text-gray-900 dark:text-gray-200">
                              {column.name
                                ? `${column.name} (${column.type})`
                                : column.type}
                            </h5>
                            <div className="text-sm text-gray-500 dark:text-gray-400">
                              Type: {column.value_type}
                            </div>
                          </div>
                          <div className="flex items-center">
                            <div className="text-sm font-medium text-gray-600 dark:text-gray-300 mr-4">
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
                          <div className="p-4 bg-white dark:bg-gray-700">
                            {/* Column Stats */}
                            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
                              <div className="bg-gray-50 dark:bg-gray-600 p-3 rounded-lg">
                                <div className="text-gray-500 dark:text-gray-400 mb-1">
                                  Compression ({column.compression})
                                </div>
                                <div className="font-medium whitespace-nowrap">
                                  <CompressionRatio
                                    compressed={column.compressed_size}
                                    uncompressed={column.uncompressed_size}
                                  />
                                </div>
                                <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                  {formatBytes(column.compressed_size)} →{" "}
                                  {formatBytes(column.uncompressed_size)}
                                </div>
                              </div>
                              <div className="bg-gray-50 dark:bg-gray-600 p-3 rounded-lg">
                                <div className="text-gray-500 dark:text-gray-400 mb-1">
                                  Rows
                                </div>
                                <div className="font-medium">
                                  {column.rows_count.toLocaleString()}
                                </div>
                              </div>
                              <div className="bg-gray-50 dark:bg-gray-600 p-3 rounded-lg">
                                <div className="text-gray-500 dark:text-gray-400 mb-1">
                                  Values Count
                                </div>
                                <div className="font-medium">
                                  {column.values_count.toLocaleString()}
                                </div>
                              </div>
                              <div className="bg-gray-50 dark:bg-gray-600 p-3 rounded-lg">
                                <div className="text-gray-500 dark:text-gray-400 mb-1">
                                  Offset
                                </div>
                                <div className="font-medium">
                                  {formatBytes(column.metadata_offset)}
                                </div>
                              </div>
                            </div>

                            {/* Pages */}
                            {column.pages.length > 0 && (
                              <div className="mt-6">
                                <h6 className="font-medium text-sm mb-3 dark:text-gray-200">
                                  Pages ({column.pages.length})
                                </h6>
                                <div className="overflow-x-auto rounded-lg border border-gray-200 dark:border-gray-600">
                                  <table className="min-w-full text-sm">
                                    <thead>
                                      <tr className="bg-gray-50 dark:bg-gray-600 border-b border-gray-200 dark:border-gray-500">
                                        <th className="text-left p-3 font-medium text-gray-600 dark:text-gray-200">
                                          #
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600 dark:text-gray-200">
                                          Rows
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600 dark:text-gray-200">
                                          Values
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600 dark:text-gray-200">
                                          Encoding
                                        </th>
                                        <th className="text-left p-3 font-medium text-gray-600 dark:text-gray-200">
                                          Compression
                                        </th>
                                      </tr>
                                    </thead>
                                    <tbody className="bg-white dark:bg-gray-700">
                                      {column.pages.map((page, pageIndex) => (
                                        <tr
                                          key={pageIndex}
                                          className="border-t border-gray-100 dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-600"
                                        >
                                          <td className="p-3 dark:text-gray-200">
                                            {pageIndex + 1}
                                          </td>
                                          <td className="p-3 dark:text-gray-200">
                                            {page.rows_count.toLocaleString()}
                                          </td>
                                          <td className="p-3 dark:text-gray-200">
                                            {page.values_count.toLocaleString()}
                                          </td>
                                          <td className="p-3 dark:text-gray-200">
                                            {page.encoding}
                                          </td>
                                          <td className="p-3">
                                            <div className="flex items-center gap-2">
                                              <CompressionRatio
                                                compressed={
                                                  page.compressed_size
                                                }
                                                uncompressed={
                                                  page.uncompressed_size
                                                }
                                              />
                                              <span className="text-xs text-gray-500 dark:text-gray-400">
                                                (
                                                {formatBytes(
                                                  page.compressed_size
                                                )}{" "}
                                                →{" "}
                                                {formatBytes(
                                                  page.uncompressed_size
                                                )}
                                                )
                                              </span>
                                            </div>
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
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};
