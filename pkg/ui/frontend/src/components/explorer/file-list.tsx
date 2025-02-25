import { useNavigate, useSearchParams, Link } from "react-router-dom";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { FolderIcon, FileIcon, DownloadIcon } from "lucide-react";
import { ExplorerFile } from "@/types/explorer";
import { formatBytes } from "@/lib/utils";
import { DateHover } from "../common/date-hover";
import { Button } from "../ui/button";

interface FileListProps {
  current: string;
  parent: string | null;
  files: ExplorerFile[];
  folders: string[];
}

export function FileList({ current, parent, files, folders }: FileListProps) {
  const navigate = useNavigate();
  const [, setSearchParams] = useSearchParams();

  const handleNavigate = (path: string) => {
    setSearchParams({ path });
  };

  const handleFileClick = (file: ExplorerFile) => {
    navigate(
      `/storage/dataobj/metadata?path=${encodeURIComponent(
        current + "/" + file.name
      )}`
    );
  };

  return (
    <div className="space-y-4">
      <Table>
        <TableHeader>
          <TableRow className="h-12">
            <TableHead>Name</TableHead>
            <TableHead>Modified</TableHead>
            <TableHead>Size</TableHead>
            <TableHead></TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {parent !== current && (
            <TableRow
              key="parent"
              className="h-12 cursor-pointer hover:bg-muted/50"
              onClick={() => handleNavigate(parent || "")}
            >
              <TableCell className="font-medium">
                <div className="flex items-center">
                  <svg
                    className="w-5 h-5 mr-2"
                    fill="none"
                    stroke="currentColor"
                    viewBox="0 0 24 24"
                  >
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth="2"
                      d="M15 19l-7-7 7-7"
                    />
                  </svg>
                  ..
                </div>
              </TableCell>
              <TableCell>-</TableCell>
              <TableCell>-</TableCell>
              <TableCell></TableCell>
            </TableRow>
          )}
          {folders.map((folder) => (
            <TableRow
              key={folder}
              className="h-12 cursor-pointer hover:bg-muted/50"
              onClick={() =>
                handleNavigate(current ? `${current}/${folder}` : folder)
              }
            >
              <TableCell className="font-medium">
                <div className="flex items-center">
                  <FolderIcon className="mr-2 h-4 w-4" />
                  {folder}
                </div>
              </TableCell>
              <TableCell>-</TableCell>
              <TableCell>-</TableCell>
              <TableCell></TableCell>
            </TableRow>
          ))}

          {files.map((file) => (
            <TableRow
              key={file.name}
              className="h-12 cursor-pointer hover:bg-muted/50"
              onClick={(e) => {
                if ((e.target as HTMLElement).closest("a[download]")) {
                  return;
                }
                handleFileClick(file);
              }}
            >
              <TableCell className="font-medium">
                <div className="flex items-center">
                  <FileIcon className="mr-2 h-4 w-4" />
                  {file.name}
                </div>
              </TableCell>
              <TableCell>
                <DateHover date={new Date(file.lastModified)} />
              </TableCell>
              <TableCell>{formatBytes(file.size)}</TableCell>
              <TableCell>
                <Button
                  variant="outline"
                  size="icon"
                  asChild
                  className="h-8 w-8"
                >
                  <Link
                    to={file.downloadUrl}
                    target="_blank"
                    download
                    onClick={(e) => e.stopPropagation()}
                  >
                    <DownloadIcon className="h-4 w-4" />
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
