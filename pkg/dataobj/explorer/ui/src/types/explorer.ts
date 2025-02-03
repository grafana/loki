export interface FileInfo {
  name: string;
  size: number;
  lastModified: string;
}

export interface ListResponse {
  files: FileInfo[];
  folders: string[];
  parent: string;
  current: string;
}
