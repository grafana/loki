import React from "react";
import { useSearchParams } from "react-router-dom";
import { FileList } from "../components/explorer/FileList";
import { Layout } from "../components/layout/Layout";
import { LoadingContainer } from "../components/common/LoadingContainer";
import { ErrorContainer } from "../components/common/ErrorContainer";
import { useExplorerData } from "../hooks/useExplorerData";

export const ExplorerPage: React.FC = () => {
  const [searchParams] = useSearchParams();
  const path = searchParams.get("path") || "";
  const { data, loading, error } = useExplorerData(path);

  // Get path parts for breadcrumb
  const pathParts = React.useMemo(
    () => (data?.current || "").split("/").filter(Boolean),
    [data?.current]
  );

  return (
    <Layout breadcrumbParts={pathParts} isLastBreadcrumbClickable={true}>
      {loading ? (
        <LoadingContainer fullScreen />
      ) : error ? (
        <ErrorContainer message={error} fullScreen />
      ) : data ? (
        <FileList
          current={data.current}
          parent={data.parent}
          files={data.files}
          folders={data.folders}
        />
      ) : null}
    </Layout>
  );
};
