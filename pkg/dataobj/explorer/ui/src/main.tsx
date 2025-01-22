import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter as Router, Routes, Route } from "react-router-dom";
import App from "./App";
import "./index.css";
import { BasenameProvider } from "./contexts/BasenameContext";

// Extract basename from current URL by matching everything up to and including /dataobj/explorer
const pathname = window.location.pathname;
const match = pathname.match(/(.*\/dataobj\/explorer\/)/);
const basename =
  match?.[0] != "/dataobj/explorer/"
    ? match?.[1] + "/dataobj/explorer/"
    : "/dataobj/explorer/";

console.log({
  pathname,
  match,
  basename,
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <BasenameProvider basename={basename}>
      <Router basename={basename}>
        <Routes>
          <Route path="*" element={<App />} />
        </Routes>
      </Router>
    </BasenameProvider>
  </React.StrictMode>
);
