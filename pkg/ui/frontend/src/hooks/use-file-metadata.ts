import { useQuery } from "@tanstack/react-query";
import { findNodeName } from "@/lib/utils";
import { useCluster } from "@/contexts/use-cluster";
import { useMemo } from "react";
import { FileMetadataResponse } from "@/types/explorer";
import { absolutePath } from "@/util";
export function useFileMetadata(path: string | undefined) {
  const { cluster } = useCluster();
  const nodeName = useMemo(() => {
    return findNodeName(cluster?.members, "dataobj-explorer");
  }, [cluster?.members]);
  const downloadUrl = useMemo(() => {
    return `/api/v1/proxy/${nodeName}/dataobj/api/v1/download?file=${encodeURIComponent(
      path || ""
    )}`;
  }, [path, nodeName]);
  const query = useQuery<FileMetadataResponse>({
    queryKey: ["file-metadata", path, nodeName],
    queryFn: async () => {
      if (!path) throw new Error("No file path provided");
      if (!nodeName) throw new Error("Node name not found");
      const response = await fetch(
        absolutePath(
          `/api/v1/proxy/${nodeName}/dataobj/api/v1/inspect?file=${encodeURIComponent(
            path
          )}`
        )
      );
      if (!response.ok) {
        throw new Error("Failed to fetch file metadata");
      }
      return response.json();
    },
    enabled: !!path && !!nodeName,
  });

  return { ...query, downloadUrl };
}
