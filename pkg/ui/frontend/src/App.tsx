import { Routes, Route } from "react-router-dom";
import { AppLayout } from "./layout/layout";
import { ThemeProvider } from "./features/theme/components/theme-provider";
import { QueryProvider } from "./providers/query-provider";
import { ClusterProvider } from "./contexts/cluster-provider";
import { FeatureFlagsProvider, useFeatureFlags } from "./contexts/feature-flags";
import { routes } from "./config/routes";

function AppRoutes() {
  const { features } = useFeatureFlags();
  
  // Filter routes based on feature flags
  const availableRoutes = routes.filter(route => {
    // Only filter out goldfish route if feature is disabled
    if (route.path === '/goldfish' && !features.goldfish) {
      return false;
    }
    return true;
  });
  
  return (
    <Routes>
      {availableRoutes.map((route) => (
        <Route
          key={route.path}
          path={route.path}
          element={route.element}
        />
      ))}
    </Routes>
  );
}

export default function App() {
  return (
    <QueryProvider>
      <ThemeProvider defaultTheme="dark" storageKey="loki-ui-theme">
        <FeatureFlagsProvider>
          <ClusterProvider>
            <AppLayout>
              <AppRoutes />
            </AppLayout>
          </ClusterProvider>
        </FeatureFlagsProvider>
      </ThemeProvider>
    </QueryProvider>
  );
}
