export interface ExplorerFile {
  name: string;
  size: number;
  lastModified: string;
  downloadUrl: string;
}

export interface ExplorerData {
  current: string;
  parent: string | null;
  files: ExplorerFile[];
  folders: string[];
}

export interface FileMetadata {
  size: number;
  modified: string;
  type: string;
  [key: string]: unknown;
}
