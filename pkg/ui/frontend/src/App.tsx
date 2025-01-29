import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./layout/layout";
import Nodes from "./pages/nodes";
import { ThemeProvider } from "./components/theme-provider";
// TODO: Add Navigation and Layout
// import Navigation from "./components/Navigation";
// import Layout from "./components/Layout";

const App = () => {
  return (
    <ThemeProvider defaultTheme="light" storageKey="loki-ui-theme">
      <AppLayout>
        {/* // <Navigation /> */}
        <Routes>
          <Route path="/" element={<Nodes />} />
        </Routes>
      </AppLayout>
    </ThemeProvider>
  );
};

export default App;
