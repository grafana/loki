import React from "react";
import { Routes, Route } from "react-router-dom";
import { FileMetadataPage } from "./pages/FileMetadataPage";
import { ExplorerPage } from "./pages/ExplorerPage";

export default function App() {
  return (
    <Routes>
      <Route path="dataobj/explorer/" element={<ExplorerPage />} />
      <Route
        path="dataobj/explorer/file/:filePath"
        element={<FileMetadataPage />}
      />
    </Routes>
  );
}
