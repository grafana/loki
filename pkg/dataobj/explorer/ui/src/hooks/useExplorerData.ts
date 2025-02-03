import React, { useMemo } from "react";
import { useBasename } from "../contexts/BasenameContext";
import { ListResponse, FileInfo } from "../types/explorer";

const sortFilesByDate = (files: FileInfo[]): FileInfo[] => {
  return [...files].sort(
    (a, b) =>
      new Date(b.lastModified).getTime() - new Date(a.lastModified).getTime()
  );
};

interface UseExplorerDataResult {
  data: ListResponse | null;
  loading: boolean;
  error: string | null;
}

export const useExplorerData = (path: string): UseExplorerDataResult => {
  const [rawData, setRawData] = React.useState<ListResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const basename = useBasename();

  // Memoize the sorted data
  const data = useMemo(() => {
    if (!rawData) return null;
    return {
      ...rawData,
      files: sortFilesByDate(rawData.files),
    };
  }, [rawData]);

  React.useEffect(() => {
    const fetchData = async () => {
      try {
        setLoading(true);
        const response = await fetch(
          `${basename}api/list?path=${encodeURIComponent(path)}`
        );
        if (!response.ok) {
          throw new Error("Failed to fetch data");
        }
        const json = (await response.json()) as ListResponse;
        setRawData(json);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : "An error occurred");
      } finally {
        setLoading(false);
      }
    };

    fetchData();
  }, [path, basename]);

  return { data, loading, error };
};
