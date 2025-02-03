import {
  NodeBreadcrumb,
  RingBreadcrumb,
} from "@/components/shared/route-breadcrumbs";
import { NotFound } from "@/components/shared/errors/not-found";
import * as React from "react";
import { RouteObject } from "react-router-dom";
import { DataObjectsPage } from "@/pages/data-objects";
import { FileMetadataPage } from "@/pages/file-metadata";

// Routes configuration for breadcrumbs
export const routes: RouteObject[] = [
  {
    path: "/",
    handle: { breadcrumb: "Home" },
  },
  {
    path: "/nodes",
    handle: { breadcrumb: "Nodes" },
  },
  {
    path: "/nodes/:nodeName",
    handle: { breadcrumb: NodeBreadcrumb },
  },
  {
    path: "/rings",
    handle: { breadcrumb: "Rings" },
  },
  {
    path: "/rings/:ringName",
    handle: { breadcrumb: RingBreadcrumb },
  },
  {
    path: "/storage",
    handle: { breadcrumb: "Storage" },
  },
  {
    path: "/storage/object",
    handle: { breadcrumb: "Object Storage" },
  },
  {
    path: "/storage/data",
    handle: { breadcrumb: "Data Objects" },
  },
  {
    path: "/storage/dataobj",
    element: <DataObjectsPage />,
    handle: { breadcrumb: "Data Objects" },
  },
  {
    path: "/storage/dataobj/metadata/:filePath",
    element: <FileMetadataPage />,
    handle: { breadcrumb: "File Metadata" },
  },
  {
    path: "/tenants",
    handle: { breadcrumb: "Tenants" },
  },
  {
    path: "/tenants/limits",
    handle: { breadcrumb: "Limits" },
  },
  {
    path: "/tenants/labels",
    handle: { breadcrumb: "Labels" },
  },
  {
    path: "/rules",
    handle: { breadcrumb: "Rules" },
  },
  {
    path: "/404",
    handle: { breadcrumb: "404" },
    element: <NotFound />,
  },
  {
    path: "*",
    handle: { breadcrumb: "404" },
    element: <NotFound />,
  },
];
