import React from "react";
import { Link } from "react-router-dom";

export const BackToListButton: React.FC<{ filePath: string }> = ({
  filePath,
}) => (
  <Link
    to={`/?path=${encodeURIComponent(
      filePath ? filePath.split("/").slice(0, -1).join("/") : ""
    )}`}
    className="mb-4 p-4 inline-flex items-center text-sm font-medium text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300"
  >
    <svg
      className="w-4 h-4 mr-1"
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
    Back to file list
  </Link>
);
