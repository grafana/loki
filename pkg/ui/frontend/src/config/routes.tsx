import {
  NodeBreadcrumb,
  RingBreadcrumb,
} from "@/components/shared/route-breadcrumbs";
import { NotFound } from "@/components/shared/errors/not-found";
import Ring from "@/pages/ring";
import { RouteObject } from "react-router-dom";
import { DataObjectsPage } from "@/pages/data-objects";
import { FileMetadataPage } from "@/pages/file-metadata";
import Nodes from "@/pages/nodes";
import { BreadcrumbComponentType } from "use-react-router-breadcrumbs";
import NodeDetails from "@/pages/node-details";
import ComingSoon from "@/pages/coming-soon";
import DeletesPage from "@/pages/deletes";
import NewDeleteRequest from "@/pages/new-delete";
import AnalyzeLabels from "@/pages/analyze-labels";

type RouteObjectWithBreadcrumb = Omit<RouteObject, "children"> & {
  breadcrumb: string | BreadcrumbComponentType;
};

// Routes configuration for breadcrumbs
export const routes: RouteObjectWithBreadcrumb[] = [
  {
    path: "/",
    breadcrumb: "Home",
    element: <Nodes />,
  },
  {
    path: "/nodes",
    breadcrumb: "Nodes",
    element: <Nodes />,
  },
  {
    path: "/nodes/:nodeName",
    breadcrumb: NodeBreadcrumb,
    element: <NodeDetails />,
  },
  {
    path: "/versions",
    breadcrumb: "Versions",
    element: <ComingSoon />,
  },
  {
    path: "/rings",
    breadcrumb: "Rings",
    element: <Ring />,
  },
  {
    path: "/rings/:ringName",
    breadcrumb: RingBreadcrumb,
    element: <Ring />,
  },
  {
    path: "/storage",
    breadcrumb: "Storage",
    element: <ComingSoon />,
  },
  {
    path: "/storage/object",
    breadcrumb: "Object Storage",
    element: <ComingSoon />,
  },
  {
    path: "/storage/dataobj",
    breadcrumb: "Data Objects",
    element: <DataObjectsPage />,
  },
  {
    path: "/storage/dataobj/metadata",
    breadcrumb: "File Metadata",
    element: <FileMetadataPage />,
  },
  {
    path: "/tenants",
    breadcrumb: "Tenants",
    element: <ComingSoon />,
  },
  {
    path: "/tenants/deletes",
    breadcrumb: "Deletes",
    element: <DeletesPage />,
  },
  {
    path: "/tenants/deletes/new",
    element: <NewDeleteRequest />,
    breadcrumb: "New Delete Request",
  },
  {
    path: "/tenants/analyze-labels",
    element: <AnalyzeLabels />,
    breadcrumb: "Analyze Labels",
  },
  {
    path: "/tenants/limits",
    breadcrumb: "Limits",
    element: <ComingSoon />,
  },
  {
    path: "/tenants/labels",
    breadcrumb: "Labels",
    element: <ComingSoon />,
  },
  {
    path: "/rules",
    breadcrumb: "Rules",
    element: <ComingSoon />,
  },
  {
    path: "/404",
    breadcrumb: "404",
    element: <NotFound />,
  },
];
