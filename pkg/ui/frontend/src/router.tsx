import * as React from "react";
import { createBrowserRouter } from "react-router-dom";
import { routes } from "./config/routes";

export const router = createBrowserRouter(routes, {
  basename: "/loki",
});
