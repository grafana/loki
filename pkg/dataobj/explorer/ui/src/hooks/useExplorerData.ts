import React from "react";
import { useBasename } from "../contexts/BasenameContext";
import { ListResponse } from "../types/explorer";

interface UseExplorerDataResult {
  data: ListResponse | null;
  loading: boolean;
  error: string | null;
}

export const useExplorerData = (path: string): UseExplorerDataResult => {
  const [data, setData] = React.useState<ListResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const basename = useBasename();

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
  }, [path, basename]);

  return { data, loading, error };
};
