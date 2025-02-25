import { useSearchParams } from "react-router-dom";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { FileList } from "@/components/explorer/file-list";
import { useExplorerData } from "@/hooks/use-explorer-data";
import { ExplorerBreadcrumb } from "@/components/explorer/breadcrumb";
import { PageContainer } from "@/layout/page-container";

export function DataObjectsPage() {
  const [searchParams] = useSearchParams();
  const path = searchParams.get("path") || "";
  const { data, isLoading, error } = useExplorerData(path);

  return (
    <PageContainer>
      <div className="flex h-full flex-col space-y-6">
        <ExplorerBreadcrumb />
        <ScrollArea className="h-full">
          <div className="grid gap-4">
            <Card>
              <CardHeader>
                <CardTitle>
                  <h2 className="text-3xl font-semibold tracking-tight">
                    Data Objects Explorer
                  </h2>
                </CardTitle>
                <CardDescription>
                  <p className="text-sm text-muted-foreground mt-1">
                    The Data Objects Explorer allows you to explore the data
                    objects in the cluster.
                  </p>
                </CardDescription>
              </CardHeader>
              <CardContent>
                {isLoading ? (
                  <div className="flex items-center justify-center p-8">
                    Loading...
                  </div>
                ) : error ? (
                  <div className="flex items-center justify-center p-8 text-destructive">
                    {error.message}
                  </div>
                ) : data ? (
                  <FileList
                    current={data.current}
                    parent={data.parent}
                    files={data.files}
                    folders={data.folders}
                  />
                ) : null}
              </CardContent>
            </Card>
          </div>
        </ScrollArea>
      </div>
    </PageContainer>
  );
}
