import React, { useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { FileList } from "../components/FileList";
import { ScrollToTopButton } from "../components/ScrollToTopButton";
import { Layout } from "../components/Layout";

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

export const ExplorerPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const path = searchParams.get("path") || "";
  const [data, setData] = useState<ListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showScrollButton, setShowScrollButton] = useState(false);

  useEffect(() => {
    const handleScroll = () => {
      setShowScrollButton(window.scrollY > 300);
    };

    window.addEventListener("scroll", handleScroll);
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

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
    const cleanPath = newPath.replace(/^\/+|\/+$/g, "");
    setSearchParams(cleanPath ? { path: cleanPath } : {});
  };

  const handleFileClick = (file: string) => {
    navigate(`/dataobj/explorer/file/${encodeURIComponent(file)}`);
  };

  if (loading) {
    return (
      <Layout>
        <div className="flex items-center justify-center min-h-screen dark:bg-gray-900">
          <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-blue-500 dark:border-blue-400"></div>
        </div>
      </Layout>
    );
  }

  if (error) {
    return (
      <Layout>
        <div className="flex items-center justify-center min-h-screen">
          <div className="text-red-500">Error: {error}</div>
        </div>
      </Layout>
    );
  }

  if (!data) {
    return null;
  }

  // Get path parts for breadcrumb
  const pathParts = (data.current || "").split("/").filter(Boolean);

  return (
    <Layout
      breadcrumbParts={pathParts}
      onBreadcrumbNavigate={navigateTo}
      isLastBreadcrumbClickable={true}
    >
      <FileList
        current={data.current}
        parent={data.parent}
        files={data.files}
        folders={data.folders}
        onNavigate={navigateTo}
        onFileSelect={handleFileClick}
      />
      <ScrollToTopButton show={showScrollButton} />
    </Layout>
  );
};
