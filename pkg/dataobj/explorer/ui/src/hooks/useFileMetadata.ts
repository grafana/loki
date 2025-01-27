import React from "react";
import { useBasename } from "../contexts/BasenameContext";
import { FileMetadataResponse } from "../types/metadata";

interface UseFileMetadataResult {
  metadata: FileMetadataResponse | null;
  loading: boolean;
  error: string | null;
}

export const useFileMetadata = (
  filePath: string | undefined
): UseFileMetadataResult => {
  const [metadata, setMetadata] = React.useState<FileMetadataResponse | null>(
    null
  );
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState<string | null>(null);
  const basename = useBasename();

  React.useEffect(() => {
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
  }, [filePath, basename]);

  return { metadata, loading, error };
};
