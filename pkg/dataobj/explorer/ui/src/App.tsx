import React, { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { FileMetadata } from "./components/FileMetadata";

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
  const [selectedFileMetadata, setSelectedFileMetadata] = useState(null);
  const [selectedFile, setSelectedFile] = useState<string | null>(null);
  const [showScrollButton, setShowScrollButton] = useState(false);
  const [fileMetadataLoading, setFileMetadataLoading] = useState(false);
  const [isDarkMode, setIsDarkMode] = useState(() => {
    // Check if user has a saved preference
    const savedTheme = localStorage.getItem("theme");
    // Check system preference if no saved preference
    const systemPreference = window.matchMedia(
      "(prefers-color-scheme: dark)"
    ).matches;
    return savedTheme ? savedTheme === "dark" : systemPreference;
  });

  useEffect(() => {
    const handleScroll = () => {
      setShowScrollButton(window.scrollY > 300);
    };

    window.addEventListener("scroll", handleScroll);
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  useEffect(() => {
    // Update document class when theme changes
    document.documentElement.classList.toggle("dark", isDarkMode);
    // Save preference
    localStorage.setItem("theme", isDarkMode ? "dark" : "light");
  }, [isDarkMode]);

  const scrollToTop = () => {
    window.scrollTo({
      top: 0,
      behavior: "smooth",
    });
  };

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
    setSelectedFileMetadata(null);
    setSelectedFile(null);
    // Remove leading/trailing slashes
    const cleanPath = newPath.replace(/^\/+|\/+$/g, "");
    setSearchParams(cleanPath ? { path: cleanPath } : {});
  };

  const handleFileClick = async (file: string) => {
    try {
      setSelectedFile(file);
      setFileMetadataLoading(true);
      const response = await fetch(
        `api/inspect?file=${encodeURIComponent(file)}`
      );
      if (!response.ok) {
        throw new Error(`Failed to fetch metadata: ${response.statusText}`);
      }
      const metadata = await response.json();
      setSelectedFileMetadata(metadata);
    } catch (error) {
      console.error("Failed to fetch file metadata:", error);
    } finally {
      setFileMetadataLoading(false);
    }
  };

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-screen dark:bg-gray-900">
        <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-blue-500 dark:border-blue-400"></div>
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
    <div
      className={`min-h-screen ${
        isDarkMode
          ? "dark:bg-gray-900 dark:text-gray-200"
          : "bg-white text-black"
      }`}
    >
      <div className="container mx-auto px-4 py-8">
        {/* Theme toggle button - positioned in top right */}
        <div className="absolute top-4 right-4">
          <button
            onClick={() => setIsDarkMode(!isDarkMode)}
            className="p-2 rounded-lg bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600 transition-colors"
            aria-label={
              isDarkMode ? "Switch to light mode" : "Switch to dark mode"
            }
          >
            {isDarkMode ? (
              <svg
                className="w-6 h-6"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth="2"
                  d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z"
                />
              </svg>
            ) : (
              <svg
                className="w-6 h-6"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth="2"
                  d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"
                />
              </svg>
            )}
          </button>
        </div>

        <div className="mb-4">
          <nav className="flex" aria-label="Breadcrumb">
            <ol className="inline-flex items-center space-x-1 md:space-x-3">
              <li className="inline-flex items-center">
                <button
                  onClick={() => navigateTo("")}
                  className="inline-flex items-center text-sm font-medium text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400"
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
                      className="ml-1 text-sm font-medium text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400 md:ml-2"
                    >
                      {part}
                    </button>
                  </div>
                </li>
              ))}
              {selectedFile && (
                <li>
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
                    <span className="ml-1 text-sm font-medium text-gray-500 md:ml-2">
                      {selectedFile.split("/").pop()}
                    </span>
                  </div>
                </li>
              )}
            </ol>
          </nav>
        </div>

        {fileMetadataLoading ? (
          <div className="flex items-center justify-center min-h-[200px] dark:bg-gray-900">
            <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-blue-500 dark:border-blue-400"></div>
          </div>
        ) : selectedFileMetadata ? (
          <div className="bg-white dark:bg-gray-800 shadow-md rounded-lg overflow-hidden dark:text-gray-200">
            <button
              onClick={() => {
                setSelectedFileMetadata(null);
                setSelectedFile(null);
              }}
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
            </button>
            <FileMetadata
              metadata={selectedFileMetadata}
              filename={selectedFile || ""}
              className="dark:bg-gray-800 dark:text-gray-200 [&>div]:dark:bg-gray-800 [&>div:nth-child(even)]:dark:bg-gray-700 [&>div>div]:dark:bg-gray-800 [&>div>div:nth-child(even)]:dark:bg-gray-700"
              onDownload={() => {
                if (selectedFile) {
                  window.open(
                    `api/download?file=${encodeURIComponent(selectedFile)}`,
                    "_blank"
                  );
                }
              }}
            />
          </div>
        ) : (
          <div className="bg-white dark:bg-gray-800 shadow-md rounded-lg overflow-hidden">
            <div className="grid grid-cols-12 bg-gray-50 dark:bg-gray-700 border-b dark:border-gray-600">
              <div className="col-span-8 p-4 font-semibold text-gray-600 dark:text-gray-200">
                Name
              </div>
              <div className="col-span-4 p-4 font-semibold text-gray-600 dark:text-gray-200">
                Size
              </div>
            </div>

            {data.parent !== data.current && (
              <div
                onClick={() => navigateTo(data.parent)}
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

            {data.folders.map((folder) => (
              <div
                key={folder}
                onClick={() =>
                  navigateTo(
                    data.current ? `${data.current}/${folder}` : folder
                  )
                }
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

            {/* Files */}
            <div className="space-y-2">
              {data.files.map((file) => (
                <div
                  key={file.name}
                  className="grid grid-cols-12 border-b dark:border-gray-600 hover:bg-gray-50 dark:hover:bg-gray-700 cursor-pointer dark:text-gray-200"
                  onClick={() =>
                    handleFileClick(
                      data.current ? `${data.current}/${file.name}` : file.name
                    )
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
        )}

        {showScrollButton && (
          <button
            onClick={scrollToTop}
            className="fixed bottom-8 right-8 bg-blue-500 dark:bg-blue-600 hover:bg-blue-600 dark:hover:bg-blue-700 text-white rounded-full p-3 shadow-lg transition-all duration-300"
            aria-label="Back to top"
          >
            <svg
              className="w-6 h-6"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth="2"
                d="M5 10l7-7m0 0l7 7m-7-7v18"
              />
            </svg>
          </button>
        )}
      </div>
    </div>
  );
}
