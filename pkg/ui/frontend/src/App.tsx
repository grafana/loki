import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./layout/layout";
import Nodes from "./pages/nodes";
import NodeDetails from "./pages/node-details";
import ComingSoon from "./pages/coming-soon";
import { ThemeProvider } from "./features/theme";
import { ClusterProvider } from "./contexts/cluster-provider";
import { NotFound } from "./components/shared/errors/not-found";

const App = () => {
  return (
    <ThemeProvider defaultTheme="light" storageKey="loki-ui-theme">
      <ClusterProvider>
        <AppLayout>
          {/* // <Navigation /> */}
          <Routes>
            <Route path="/" element={<Nodes />} />
            <Route path="/nodes" element={<Nodes />} />
            <Route path="/nodes/:nodeName" element={<NodeDetails />} />
            <Route path="/versions" element={<ComingSoon />} />
            <Route path="/rings/*" element={<ComingSoon />} />
            <Route path="/storage/*" element={<ComingSoon />} />
            <Route path="/tenants/*" element={<ComingSoon />} />
            <Route path="/rules" element={<ComingSoon />} />
            <Route path="*" element={<NotFound />} />
          </Routes>
        </AppLayout>
      </ClusterProvider>
    </ThemeProvider>
  );
};

export default App;
