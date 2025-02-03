import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./layout/layout";
import { ThemeProvider } from "./features/theme/components/theme-provider";
import { QueryProvider } from "./providers/query-provider";
import { ClusterProvider } from "./contexts/cluster-provider";
import { NotFound } from "./components/shared/errors/not-found";
import { DataObjectsPage } from "./pages/data-objects";
import { FileMetadataPage } from "./pages/file-metadata";
import Nodes from "./pages/nodes";
import ComingSoon from "./pages/coming-soon";
import Ring from "./pages/ring";
import NodeDetails from "./pages/node-details";

export default function App() {
  return (
    <QueryProvider>
      <ThemeProvider defaultTheme="dark" storageKey="loki-ui-theme">
        <ClusterProvider>
          <AppLayout>
            <Routes>
              <Route path="/" element={<Nodes />} />
              <Route path="/nodes" element={<Nodes />} />
              <Route path="/nodes/:nodeName" element={<NodeDetails />} />
              <Route path="/versions" element={<ComingSoon />} />
              <Route path="/rings" element={<Ring />} />
              <Route path="/rings/:ringName" element={<Ring />} />
              <Route path="/storage/dataobj" element={<DataObjectsPage />} />
              <Route path="/storage/*" element={<ComingSoon />} />
              <Route path="/tenants/*" element={<ComingSoon />} />
              <Route path="/rules" element={<ComingSoon />} />
              <Route path="/storage/dataobj" element={<DataObjectsPage />} />
              <Route
                path="/storage/dataobj/metadata/:filePath"
                element={<FileMetadataPage />}
              />
              <Route path="/404" element={<NotFound />} />
            </Routes>
          </AppLayout>
        </ClusterProvider>
      </ThemeProvider>
    </QueryProvider>
  );
}
