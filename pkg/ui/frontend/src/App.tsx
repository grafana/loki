import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./layout/layout";
import Nodes from "./pages/nodes";
import NodeDetails from "./pages/node-details";
import ComingSoon from "./pages/coming-soon";
import { ThemeProvider } from "./components/theme-provider";

const App = () => {
  return (
    <ThemeProvider defaultTheme="light" storageKey="loki-ui-theme">
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
        </Routes>
      </AppLayout>
    </ThemeProvider>
  );
};

export default App;
