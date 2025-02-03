import { useSearchParams } from "react-router-dom";
import { BreadcrumbNav } from "@/components/shared/breadcrumb-nav";
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

export function DataObjectsPage() {
  const [searchParams] = useSearchParams();
  const path = searchParams.get("path") || "";
  const { data, isLoading, error } = useExplorerData(path);

  return (
    <div className="flex h-full flex-col gap-4 p-4">
      <div className="flex items-center justify-between">
        <BreadcrumbNav />
      </div>
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
  );
}
