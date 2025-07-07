import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./layout/layout";
import { ThemeProvider } from "./features/theme/components/theme-provider";
import { QueryProvider } from "./providers/query-provider";
import { ClusterProvider } from "./contexts/cluster-provider";
import { routes } from "./config/routes";

export default function App() {
  return (
    <QueryProvider>
      <ThemeProvider defaultTheme="dark" storageKey="loki-ui-theme">
        <ClusterProvider>
          <AppLayout>
            <Routes>
              {routes.map((route) => (
                <Route
                  key={route.path}
                  path={route.path}
                  element={route.element}
                />
              ))}
            </Routes>
          </AppLayout>
        </ClusterProvider>
      </ThemeProvider>
    </QueryProvider>
  );
}
