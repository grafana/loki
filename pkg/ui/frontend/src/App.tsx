import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./components/layout/AppLayout";
import Nodes from "./pages/Nodes";
import { ThemeProvider } from "./components/theme-provider";
// TODO: Add Navigation and Layout
// import Navigation from "./components/Navigation";
// import Layout from "./components/Layout";

const App = () => {
  return (
    <ThemeProvider defaultTheme="light" storageKey="loki-ui-theme">
      <AppLayout>
        {/* // <Navigation /> */}
        <div className="max-w-7xl mx-auto py-6 sm:px-6 lg:px-8">
          <Routes>
            <Route path="/" element={<Nodes />} />
          </Routes>
        </div>
      </AppLayout>
    </ThemeProvider>
  );
};

export default App;
