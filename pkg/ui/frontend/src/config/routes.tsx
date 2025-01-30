import {
  NodeBreadcrumb,
  RingBreadcrumb,
} from "@/components/breadcrumbs/route-breadcrumbs";

// Routes configuration for breadcrumbs
export const routes = [
  { path: "/", breadcrumb: "Home" },
  { path: "/nodes", breadcrumb: "Nodes" },
  { path: "/nodes/:nodeName", breadcrumb: NodeBreadcrumb },
  { path: "/rings", breadcrumb: "Rings" },
  { path: "/rings/:ringName", breadcrumb: RingBreadcrumb },
  { path: "/storage", breadcrumb: "Storage" },
  { path: "/storage/object", breadcrumb: "Object Storage" },
  { path: "/storage/data", breadcrumb: "Data Objects" },
  { path: "/tenants", breadcrumb: "Tenants" },
  { path: "/tenants/limits", breadcrumb: "Limits" },
  { path: "/tenants/labels", breadcrumb: "Labels" },
  { path: "/rules", breadcrumb: "Rules" },
];
