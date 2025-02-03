import * as React from "react";
import { useMatches, Link } from "react-router-dom";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

interface BreadcrumbHandle {
  breadcrumb: React.ReactNode;
}

export function BreadcrumbNav() {
  const matches = useMatches();

  const crumbs = matches
    .filter((match) => Boolean((match.handle as BreadcrumbHandle)?.breadcrumb))
    .map((match) => ({
      path: match.pathname,
      breadcrumb: (match.handle as BreadcrumbHandle).breadcrumb,
    }));

  if (crumbs.length <= 1) return null;
  console.log(crumbs);
  return (
    <Breadcrumb>
      <BreadcrumbList>
        {crumbs.map((crumb, index) => (
          <React.Fragment key={crumb.path}>
            <BreadcrumbItem className={index === 0 ? "hidden md:block" : ""}>
              {index === crumbs.length - 1 ? (
                <BreadcrumbPage>{crumb.breadcrumb}</BreadcrumbPage>
              ) : (
                <BreadcrumbLink asChild>
                  <Link to={crumb.path}>{crumb.breadcrumb}</Link>
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
            {index < crumbs.length - 1 && (
              <BreadcrumbSeparator
                className={index === 0 ? "hidden md:block" : ""}
              />
            )}
          </React.Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
