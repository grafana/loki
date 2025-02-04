import { useSearchParams } from "react-router-dom";
import { FileMetadataView } from "@/components/explorer/file-metadata";
import { useFileMetadata } from "@/hooks/use-file-metadata";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ExplorerBreadcrumb } from "@/components/explorer/breadcrumb";
import { Loader2 } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { PageContainer } from "@/layout/page-container";

export function FileMetadataPage() {
  const [searchParams] = useSearchParams();
  const path = searchParams.get("path") || "";
  const {
    data: metadata,
    downloadUrl,
    isLoading,
    error,
  } = useFileMetadata(path);

  return (
    <PageContainer>
      <div className="flex h-full flex-col space-y-6">
        <ExplorerBreadcrumb />
        <ScrollArea className="h-full">
          {isLoading ? (
            <div className="flex items-center justify-center p-8">
              <Loader2 className="h-16 w-16 animate-spin" />
            </div>
          ) : error ? (
            <Alert variant="destructive">
              <AlertTitle>Error</AlertTitle>
              <AlertDescription>{error.message}</AlertDescription>
            </Alert>
          ) : metadata && path ? (
            <FileMetadataView
              metadata={metadata}
              filename={path}
              downloadUrl={downloadUrl}
            />
          ) : null}
        </ScrollArea>
      </div>
    </PageContainer>
  );
}
