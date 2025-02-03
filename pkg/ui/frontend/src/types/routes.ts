import { RouteObject } from "react-router-dom";

export interface CustomRouteObject extends RouteObject {
  breadcrumb?: string | React.ReactNode;
}
