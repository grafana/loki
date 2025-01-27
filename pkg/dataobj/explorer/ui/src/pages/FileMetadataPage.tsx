import React from "react";
import { useParams } from "react-router-dom";
import { FileMetadata } from "../components/file-metadata/FileMetadata";
import { Layout } from "../components/layout/Layout";
import { BackToListButton } from "../components/file-metadata/BackToList";
import { LoadingContainer } from "../components/common/LoadingContainer";
import { ErrorContainer } from "../components/common/ErrorContainer";
import { useFileMetadata } from "../hooks/useFileMetadata";

export const FileMetadataPage: React.FC = () => {
  const { filePath } = useParams<{ filePath: string }>();
  const { metadata, loading, error } = useFileMetadata(filePath);
  const pathParts = React.useMemo(
    () => (filePath || "").split("/").filter(Boolean),
    [filePath]
  );

  return (
    <Layout breadcrumbParts={pathParts} isLastBreadcrumbClickable={false}>
      <div className="bg-gray-50 dark:bg-gray-800 shadow-md rounded-lg overflow-hidden dark:text-gray-200">
        {loading ? (
          <LoadingContainer />
        ) : error ? (
          <ErrorContainer message={error} />
        ) : (
          <>
            <BackToListButton filePath={filePath || ""} />
            {metadata && filePath && (
              <FileMetadata
                metadata={metadata}
                filename={filePath}
                className="dark:bg-gray-800 dark:text-gray-200"
              />
            )}
          </>
        )}
      </div>
    </Layout>
  );
};
