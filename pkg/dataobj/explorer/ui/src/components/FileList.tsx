import React from "react";
import { useNavigate } from "react-router-dom";

interface FileInfo {
  name: string;
  size: number;
}

interface FileListProps {
  current: string;
  parent: string;
  files: FileInfo[];
  folders: string[];
  onNavigate: (path: string) => void;
  onFileSelect: (path: string) => void;
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
  onNavigate,
  onFileSelect,
}) => {
  return (
    <div className="bg-white dark:bg-gray-800 shadow-md rounded-lg overflow-hidden">
      <div className="grid grid-cols-12 bg-gray-50 dark:bg-gray-700 border-b dark:border-gray-600">
        <div className="col-span-8 p-4 font-semibold text-gray-600 dark:text-gray-200">
          Name
        </div>
        <div className="col-span-4 p-4 font-semibold text-gray-600 dark:text-gray-200">
          Size
        </div>
      </div>

      {parent !== current && (
        <div
          onClick={() => onNavigate(parent)}
          className="grid grid-cols-12 border-b dark:border-gray-600 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700 dark:text-gray-200"
        >
          <div className="col-span-8 p-4 flex items-center">
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
          <div className="col-span-4 p-4">-</div>
        </div>
      )}

      {folders.map((folder) => (
        <div
          key={folder}
          onClick={() => onNavigate(current ? `${current}/${folder}` : folder)}
          className="grid grid-cols-12 border-b dark:border-gray-600 cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700 dark:text-gray-200"
        >
          <div className="col-span-8 p-4 flex items-center">
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
          <div className="col-span-4 p-4">-</div>
        </div>
      ))}

      <div className="space-y-2">
        {files.map((file) => (
          <div
            key={file.name}
            className="grid grid-cols-12 border-b dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700 cursor-pointer dark:text-gray-200"
            onClick={() =>
              onFileSelect(current ? `${current}/${file.name}` : file.name)
            }
          >
            <div className="col-span-8 p-4 flex items-center">
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
            </div>
            <div className="col-span-4 p-4">{formatBytes(file.size)}</div>
          </div>
        ))}
      </div>
    </div>
  );
};
