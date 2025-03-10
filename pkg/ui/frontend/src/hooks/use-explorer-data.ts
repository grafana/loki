import { useQuery } from "@tanstack/react-query";
import { ExplorerData, ExplorerFile } from "@/types/explorer";
import { useCluster } from "@/contexts/use-cluster";
import { useMemo } from "react";
import { findNodeName } from "@/lib/utils";
import { absolutePath } from "@/util";

// mux.HandleFunc("/api/v1/dataobj/list", s.handleList)
// mux.HandleFunc("/api/v1/dataobj/inspect", s.handleInspect)
// mux.HandleFunc("/api/v1/dataobj/download", s.handleDownload)
// mux.HandleFunc("/api/v1/dataobj/provider", s.handleProvider)

export function useExplorerData(path: string) {
  const { cluster } = useCluster();

  const nodeName = useMemo(() => {
    return findNodeName(cluster?.members, "dataobj-explorer");
  }, [cluster?.members]);

  return useQuery<ExplorerData>({
    queryKey: ["explorer", path, nodeName],
    queryFn: async () => {
      if (!nodeName) {
        throw new Error("Node name not found");
      }
      const response = await fetch(
        absolutePath(
          `/api/v1/proxy/${nodeName}/dataobj/api/v1/list?path=${encodeURIComponent(
            path
          )}`
        )
      );
      if (!response.ok) {
        throw new Error("Failed to fetch explorer data");
      }
      const data = (await response.json()) as ExplorerData;
      return {
        ...data,
        files: sortFilesByDate(data.files).map((file) => ({
          ...file,
          downloadUrl: absolutePath(
            `/api/v1/proxy/${nodeName}/dataobj/api/v1/download?file=${encodeURIComponent(
              path ? `${path}/${file.name}` : file.name
            )}`
          ),
        })),
      };
    },
  });
}

const sortFilesByDate = (files: ExplorerFile[]): ExplorerFile[] => {
  return [...files].sort(
    (a, b) =>
      new Date(b.lastModified).getTime() - new Date(a.lastModified).getTime()
  );
};
