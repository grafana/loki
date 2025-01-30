import { useCluster } from "@/contexts/use-cluster";
import { useEffect, useState } from "react";

interface VersionInfo {
  revision: string;
  branch: string;
  buildUser: string;
  buildDate: string;
  goVersion: string;
}

interface UseVersionInfoResult {
  mostCommonVersion: string;
  versionInfos: Array<{
    version: string;
    info: VersionInfo;
  }>;
  isLoading: boolean;
}

export function useVersionInfo(): UseVersionInfoResult {
  const { cluster, isLoading: clusterLoading } = useCluster();
  const [debouncedLoading, setDebouncedLoading] = useState(true);

  useEffect(() => {
    let timeoutId: NodeJS.Timeout;

    if (clusterLoading) {
      setDebouncedLoading(true);
    } else {
      // When loading finishes, keep the loading state for a minimum duration
      timeoutId = setTimeout(() => {
        setDebouncedLoading(false);
      }, 500); // Keep loading state visible for 500ms after data loads
    }

    return () => {
      if (timeoutId) {
        clearTimeout(timeoutId);
      }
    };
  }, [clusterLoading]);

  const getMostCommonVersion = (): string => {
    if (!cluster?.members) return "v0.0.0";

    const versionCounts = new Map<string, number>();
    Object.values(cluster.members).forEach((member) => {
      if (!member.build.version) return;
      const version = member.build.version;
      versionCounts.set(version, (versionCounts.get(version) || 0) + 1);
    });

    let mostCommonVersion = "v0.0.0";
    let maxCount = 0;

    versionCounts.forEach((count, version) => {
      if (count > maxCount) {
        maxCount = count;
        mostCommonVersion = version;
      }
    });

    return mostCommonVersion;
  };

  const getVersionInfos = (): Array<{ version: string; info: VersionInfo }> => {
    if (!cluster?.members) return [];

    const versions = new Set<string>();
    const buildInfo = new Map<string, VersionInfo>();

    Object.values(cluster.members).forEach((member) => {
      const version = member.build.version;
      versions.add(version);
      buildInfo.set(version, {
        revision: member.build.revision,
        branch: member.build.branch,
        buildUser: member.build.buildUser,
        buildDate: member.build.buildDate,
        goVersion: member.build.goVersion,
      });
    });

    return Array.from(versions).map((version) => ({
      version: version ?? "v0.0.0",
      info: buildInfo.get(version) ?? {
        revision: "v0.0.0",
        branch: "v0.0.0",
        buildUser: "v0.0.0",
        buildDate: "v0.0.0",
        goVersion: "v0.0.0",
      },
    }));
  };

  return {
    mostCommonVersion: getMostCommonVersion(),
    versionInfos: getVersionInfos(),
    isLoading: debouncedLoading,
  };
}
