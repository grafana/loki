import React, { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

interface FileInfo {
  name: string;
  size: number;
}

interface ListResponse {
  files: FileInfo[];
  folders: string[];
  parent: string;
  current: string;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 Bytes";
  const k = 1024;
  const sizes = ["Bytes", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

export default function App() {
  const [searchParams, setSearchParams] = useSearchParams();
  const path = searchParams.get("path") || "";
  const [data, setData] = useState<ListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const response = await fetch(
          `api/list?path=${encodeURIComponent(path)}`
        );
        if (!response.ok) {
          throw new Error("Failed to fetch data");
        }
        const json = await response.json();
        setData(json);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [path]);

  const navigateTo = (newPath: string) => {
    // Remove leading/trailing slashes
    const cleanPath = newPath.replace(/^\/+|\/+$/g, "");
    setSearchParams(cleanPath ? { path: cleanPath } : {});
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-blue-500"></div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-red-500">Error: {error}</div>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  // Get path parts, filtering out empty strings
  const pathParts = (data.current || "").split("/").filter(Boolean);

  return (
    <div className="container mx-auto px-4 py-8">
      <div className="mb-4">
        <nav className="flex" aria-label="Breadcrumb">
          <ol className="inline-flex items-center space-x-1 md:space-x-3">
            <li className="inline-flex items-center">
              <button
                onClick={() => navigateTo("")}
                className="inline-flex items-center text-sm font-medium text-gray-700 hover:text-blue-600"
              >
                Root
              </button>
            </li>
            {pathParts.map((part, index, array) => (
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
                  <button
                    onClick={() =>
                      navigateTo(array.slice(0, index + 1).join("/"))
                    }
                    className="ml-1 text-sm font-medium text-gray-700 hover:text-blue-600 md:ml-2"
                  >
                    {part}
                  </button>
                </div>
              </li>
            ))}
          </ol>
        </nav>
      </div>

      <div className="bg-white shadow-md rounded-lg overflow-hidden">
        <div className="grid grid-cols-12 bg-gray-50 border-b">
          <div className="col-span-8 p-4 font-semibold text-gray-600">Name</div>
          <div className="col-span-4 p-4 font-semibold text-gray-600">Size</div>
        </div>

        {data.parent !== data.current && (
          <div
            onClick={() => navigateTo(data.parent)}
            className="grid grid-cols-12 border-b cursor-pointer hover:bg-gray-50"
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

        {data.folders.map((folder) => (
          <div
            key={folder}
            onClick={() =>
              navigateTo(data.current ? `${data.current}/${folder}` : folder)
            }
            className="grid grid-cols-12 border-b cursor-pointer hover:bg-gray-50"
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

        {data.files.map((file) => (
          <div
            key={file.name}
            className="grid grid-cols-12 border-b hover:bg-gray-50"
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
}
