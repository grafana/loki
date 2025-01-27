import React from "react";
import ReactDOM from "react-dom/client";
import {
  createBrowserRouter,
  RouterProvider,
  type RouterProviderProps,
} from "react-router-dom";
import App from "./App";
import { ExplorerPage } from "./pages/ExplorerPage";
import { FileMetadataPage } from "./pages/FileMetadataPage";
import { BasenameProvider } from "./contexts/BasenameContext";
import "./index.css";

// Extract basename from current URL by matching everything up to and including /dataobj/explorer
const pathname = window.location.pathname;
const match = pathname.match(/(.*\/dataobj\/explorer\/)/);
const basename = match?.[1] || "/dataobj/explorer/";

const router = createBrowserRouter(
  [
    {
      path: "*",
      element: <App />,
      children: [
        {
          index: true,
          element: <ExplorerPage />,
        },
        {
          path: "file/:filePath",
          element: <FileMetadataPage />,
        },
      ],
    },
  ],
  {
    basename,
    future: {
      v7_relativeSplatPath: true,
    },
  }
);

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BasenameProvider basename={basename}>
      <RouterProvider
        router={router}
        future={{
          v7_startTransition: true,
        }}
      />
    </BasenameProvider>
  </React.StrictMode>
);
