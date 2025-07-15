import * as React from "react";
import useBreadcrumbs from "use-react-router-breadcrumbs";
import { Link } from "react-router-dom";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { routes } from "@/config/routes";

export function BreadcrumbNav() {
  const breadcrumbs = useBreadcrumbs(routes, {
    disableDefaults: true,
  });

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {breadcrumbs.map(({ match, breadcrumb }, index) => (
          <React.Fragment key={match.pathname}>
            <BreadcrumbItem className={index === 0 ? "hidden md:block" : ""}>
              {index === breadcrumbs.length - 1 ? (
                <BreadcrumbPage>{breadcrumb}</BreadcrumbPage>
              ) : (
                <BreadcrumbLink asChild>
                  <Link to={match.pathname}>{breadcrumb}</Link>
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
            {index < breadcrumbs.length - 1 && (
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
