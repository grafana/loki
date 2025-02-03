import { useQuery } from "@tanstack/react-query";
import { FileMetadata } from "@/types/explorer";
import { findNodeName } from "@/lib/utils";
import { useCluster } from "@/contexts/use-cluster";

export function useFileMetadata(filePath: string | undefined) {
  const { cluster } = useCluster();
  const nodeName = findNodeName(cluster?.members, "dataobj-explorer");
  return useQuery<FileMetadata>({
    queryKey: ["file-metadata", filePath, nodeName],
    queryFn: async () => {
      if (!filePath) throw new Error("No file path provided");
      if (!nodeName) throw new Error("Node name not found");
      const response = await fetch(
        `/ui/api/v1/proxy/${nodeName}/dataobj/api/v1/inspect?path=${encodeURIComponent(
          filePath
        )}`
      );
      if (!response.ok) {
        throw new Error("Failed to fetch file metadata");
      }
      return response.json();
    },
    enabled: !!filePath && !!nodeName,
  });
}
