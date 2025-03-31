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

export interface PageInfo {
  compressed_size: number;
  uncompressed_size: number;
  rows_count: number;
  values_count: number;
  encoding: string;
  data_offset: number;
  data_size: number;
}

export interface ColumnInfo {
  name?: string;
  type: string;
  value_type: string;
  rows_count: number;
  compression: string;
  uncompressed_size: number;
  compressed_size: number;
  metadata_offset: number;
  metadata_size: number;
  values_count: number;
  pages: PageInfo[];
  statistics?: ColumnStatistics;
}

export interface SectionMetadata {
  type: string;
  totalCompressedSize: number;
  totalUncompressedSize: number;
  columnCount: number;
  columns: ColumnInfo[];
  maxTimestamp: string;
  minTimestamp: string;
  distribution: number[];
}

export interface FileMetadataResponse {
  sections: SectionMetadata[];
  error?: string;
  lastModified: string;
  minTimestamp: string;
  maxTimestamp: string;
  distribution: number[];
}

interface ColumnStatistics {
  cardinality_count?: number;
}
