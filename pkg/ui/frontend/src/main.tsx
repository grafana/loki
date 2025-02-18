import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { RouterProvider, createBrowserRouter } from "react-router-dom";

import App from "./App";
import "./index.css";
import { getBasename } from "./util";

const basename = getBasename();

const router = createBrowserRouter(
  [
    {
      path: "*",
      element: <App />,
    },
  ],
  {
    basename,
    future: {
      v7_relativeSplatPath: true,
    },
  }
);

const root = document.getElementById("root");
if (!root) throw new Error("Root element not found");

createRoot(root).render(
  <StrictMode>
    <RouterProvider
      router={router}
      future={{
        v7_startTransition: true,
      }}
    />
  </StrictMode>
);
