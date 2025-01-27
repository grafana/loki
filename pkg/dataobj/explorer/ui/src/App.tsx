import React from "react";
import { Routes, Route } from "react-router-dom";
import { FileMetadataPage } from "./pages/FileMetadataPage";
import { ExplorerPage } from "./pages/ExplorerPage";

export default function App() {
  return (
    <Routes>
      <Route path="file/:filePath" element={<FileMetadataPage />} />
      <Route path="*" element={<ExplorerPage />} />
    </Routes>
  );
}
