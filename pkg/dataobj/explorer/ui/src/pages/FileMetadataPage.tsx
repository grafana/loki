import React, { useEffect, useState } from "react";
import { useParams, useNavigate, useLocation } from "react-router-dom";
import { FileMetadata } from "../components/FileMetadata";
import { Layout } from "../components/Layout";
import { useBasename } from "../contexts/BasenameContext";

export const FileMetadataPage: React.FC = () => {
  const { filePath } = useParams<{ filePath: string }>();
  const navigate = useNavigate();
  const [metadata, setMetadata] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const basename = useBasename();

  useEffect(() => {
    const fetchMetadata = async () => {
      if (!filePath) return;
      try {
        setLoading(true);
        const response = await fetch(
          `${basename}api/inspect?file=${encodeURIComponent(filePath)}`
        );
        if (!response.ok) {
          throw new Error(`Failed to fetch metadata: ${response.statusText}`);
        }
        const data = await response.json();
        setMetadata(data);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
      } finally {
        setLoading(false);
      }
    };

    fetchMetadata();
  }, [filePath]);

  // Get path parts for breadcrumb
  const pathParts = (filePath || "").split("/").filter(Boolean);

  if (loading) {
    return (
      <Layout>
        <div className="flex items-center justify-center min-h-[200px] dark:bg-gray-900">
          <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-blue-500 dark:border-blue-400"></div>
        </div>
      </Layout>
    );
  }

  if (error) {
    return (
      <Layout>
        <div className="text-red-500 p-4">Error: {error}</div>
      </Layout>
    );
  }

  return (
    <Layout breadcrumbParts={pathParts} isLastBreadcrumbClickable={false}>
      <div className="bg-white dark:bg-gray-800 shadow-md rounded-lg overflow-hidden dark:text-gray-200">
        <button
          onClick={() => navigate(-1)}
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
        {metadata && filePath && (
          <FileMetadata
            metadata={metadata}
            filename={filePath}
            className="dark:bg-gray-800 dark:text-gray-200 [&>div]:dark:bg-gray-800 [&>div:nth-child(even)]:dark:bg-gray-700 [&>div>div]:dark:bg-gray-800 [&>div>div:nth-child(even)]:dark:bg-gray-700"
          />
        )}
      </div>
    </Layout>
  );
};
