import { useParams } from "react-router-dom";
import { BreadcrumbNav } from "@/components/shared/breadcrumb-nav";
import { FileMetadataView } from "@/components/explorer/file-metadata";
import { useFileMetadata } from "@/hooks/use-file-metadata";
import { ScrollArea } from "@/components/ui/scroll-area";

export function FileMetadataPage() {
  const { filePath } = useParams<{ filePath: string }>();
  const { data: metadata, isLoading, error } = useFileMetadata(filePath);

  return (
    <div className="flex h-full flex-col gap-4 p-4">
      <div className="flex items-center justify-between">
        <BreadcrumbNav />
      </div>
      <ScrollArea className="h-full">
        {isLoading ? (
          <div className="flex items-center justify-center p-8">Loading...</div>
        ) : error ? (
          <div className="flex items-center justify-center p-8 text-destructive">
            {error.message}
          </div>
        ) : metadata && filePath ? (
          <FileMetadataView metadata={metadata} filename={filePath} />
        ) : null}
      </ScrollArea>
    </div>
  );
}
