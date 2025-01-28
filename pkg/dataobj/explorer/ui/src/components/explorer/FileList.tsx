import React from "react";
import { Link } from "react-router-dom";
import { DateWithHover } from "../common/DateWithHover";

interface FileInfo {
  name: string;
  size: number;
  lastModified: string;
}

interface FileListProps {
  current: string;
  parent: string;
  files: FileInfo[];
  folders: string[];
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 Bytes";
  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

export const FileList: React.FC<FileListProps> = ({
  current,
  parent,
  files,
  folders,
}) => {
  return (
    <div className="bg-white dark:bg-gray-800 shadow-md rounded-lg overflow-hidden">
      <div className="grid grid-cols-12 bg-gray-50 dark:bg-gray-700 border-b dark:border-gray-600">
        <div className="col-span-5 p-4 font-semibold text-gray-600 dark:text-gray-200">
          Name
        </div>
        <div className="col-span-3 p-4 font-semibold text-gray-600 dark:text-gray-200">
          Last Modified
        </div>
        <div className="col-span-3 p-4 font-semibold text-gray-600 dark:text-gray-200">
          Size
        </div>
        <div className="col-span-1 p-4"></div>
      </div>

      {parent !== current && (
        <Link
          to={`/?path=${encodeURIComponent(parent)}`}
          className="grid grid-cols-12 border-b dark:border-gray-600 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700 dark:text-gray-200"
        >
          <div className="col-span-5 p-4 flex items-center">
            <svg
              className="w-5 h-5 mr-2"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M15 19l-7-7 7-7"
              />
            </svg>
            ..
          </div>
          <div className="col-span-3 p-4">-</div>
          <div className="col-span-3 p-4">-</div>
          <div className="col-span-1 p-4"></div>
        </Link>
      )}

      {folders.map((folder) => (
        <Link
          key={folder}
          to={`/?path=${encodeURIComponent(
            current ? `${current}/${folder}` : folder
          )}`}
          className="grid grid-cols-12 border-b dark:border-gray-600 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700 dark:text-gray-200"
        >
          <div className="col-span-5 p-4 flex items-center">
            <svg
              className="w-5 h-5 mr-2"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z"
              />
            </svg>
            {folder}
          </div>
          <div className="col-span-3 p-4">-</div>
          <div className="col-span-3 p-4">-</div>
          <div className="col-span-1 p-4"></div>
        </Link>
      ))}

      <div className="space-y-2">
        {files.map((file) => {
          const filePath = current ? `${current}/${file.name}` : file.name;

          return (
            <div
              key={file.name}
              className="grid grid-cols-12 border-b dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700 dark:text-gray-200"
            >
              <Link
                to={`file/${encodeURIComponent(filePath)}`}
                className="col-span-5 p-4 flex items-center"
              >
                <svg
                  className="w-5 h-5 mr-2"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth="2"
                    d="M7 21h10a2 2 0 002-2V9.414a1 1 0 00-.293-.707l-5.414-5.414A1 1 0 0012.586 3H7a2 2 0 00-2 2v14a2 2 0 002 2z"
                  />
                </svg>
                {file.name}
              </Link>
              <div className="col-span-3 p-4">
                <DateWithHover date={new Date(file.lastModified)} />
              </div>
              <div className="col-span-3 p-4">{formatBytes(file.size)}</div>
              <div className="col-span-1 p-4 flex justify-center">
                <Link
                  to={`/api/download?file=${encodeURIComponent(filePath)}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-gray-600 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400"
                  title="Download file"
                >
                  <svg
                    className="w-5 h-5"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth="2"
                      d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
                    />
                  </svg>
                </Link>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};
