import type { BreadcrumbComponentType } from "use-react-router-breadcrumbs";

export const NodeBreadcrumb: BreadcrumbComponentType = ({ match }) => {
  const nodeName = match.params.nodeName;
  return <span>{nodeName}</span>;
};

export const RingBreadcrumb: BreadcrumbComponentType = ({ match }) => {
  const ringName = match.params.ringName;
  return <span>{ringName}</span>;
};
