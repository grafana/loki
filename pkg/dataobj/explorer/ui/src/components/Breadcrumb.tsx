import React from "react";

interface BreadcrumbProps {
  parts: string[];
  onNavigate: (path: string) => void;
  isLastPartClickable?: boolean;
}

export const Breadcrumb: React.FC<BreadcrumbProps> = ({
  parts,
  onNavigate,
  isLastPartClickable = false,
}) => {
  return (
    <nav className="flex mb-4" aria-label="Breadcrumb">
      <ol className="inline-flex items-center space-x-1 md:space-x-3">
        <li className="inline-flex items-center">
          <button
            onClick={() => onNavigate("")}
            className="inline-flex items-center text-sm font-medium text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400"
          >
            Root
          </button>
        </li>
        {parts.map((part, index, array) => (
          <li key={index}>
            <div className="flex items-center">
              <svg
                className="w-3 h-3 text-gray-400 mx-1"
                aria-hidden="true"
                xmlns="http://www.w3.org/2000/svg"
                fill="none"
                viewBox="0 0 6 10"
              >
                <path
                  stroke="currentColor"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth="2"
                  d="m1 9 4-4-4-4"
                />
              </svg>
              {!isLastPartClickable && index === array.length - 1 ? (
                <span className="ml-1 text-sm font-medium text-gray-500 md:ml-2">
                  {part}
                </span>
              ) : (
                <button
                  onClick={() =>
                    onNavigate(array.slice(0, index + 1).join("/"))
                  }
                  className="ml-1 text-sm font-medium text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400 md:ml-2"
                >
                  {part}
                </button>
              )}
            </div>
          </li>
        ))}
      </ol>
    </nav>
  );
};
