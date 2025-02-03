import * as React from "react";
import { FileMetadata } from "@/types/explorer";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { ArrowLeftIcon } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { formatBytes } from "@/lib/utils";

interface FileMetadataViewProps {
  metadata: FileMetadata;
  filename: string;
}

export function FileMetadataView({
  metadata,
  filename,
}: FileMetadataViewProps) {
  const navigate = useNavigate();
  const basePath = filename.split("/").slice(0, -1).join("/");

  const handleBack = () => {
    navigate(`/storage/dataobj?path=${encodeURIComponent(basePath)}`);
  };

  return (
    <div className="space-y-4">
      <Button variant="outline" size="sm" onClick={handleBack}>
        <ArrowLeftIcon className="mr-2 h-4 w-4" />
        Back to list
      </Button>

      <Card>
        <CardHeader>
          <CardTitle>File Metadata</CardTitle>
          <CardDescription>{filename}</CardDescription>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <dt className="text-sm font-medium text-muted-foreground">
                Size
              </dt>
              <dd className="text-sm">{formatBytes(metadata.size)}</dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-muted-foreground">
                Modified
              </dt>
              <dd className="text-sm">{metadata.modified}</dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-muted-foreground">
                Type
              </dt>
              <dd className="text-sm">{metadata.type}</dd>
            </div>
            {Object.entries(metadata)
              .filter(([key]) => !["size", "modified", "type"].includes(key))
              .map(([key, value]) => (
                <div key={key}>
                  <dt className="text-sm font-medium text-muted-foreground">
                    {key.charAt(0).toUpperCase() + key.slice(1)}
                  </dt>
                  <dd className="text-sm">{String(value)}</dd>
                </div>
              ))}
          </dl>
        </CardContent>
      </Card>
    </div>
  );
}
