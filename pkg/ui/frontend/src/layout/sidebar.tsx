import * as React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { getBasename } from "../util";
import { VersionDisplay } from "@/components/version-display";
import { useLocation } from "react-router-dom";
import {
  ChevronDown,
  CircleDot,
  Database,
  GaugeCircle,
  LayoutDashboard,
  Users,
} from "lucide-react";

import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import { cn } from "@/lib/utils";

const data = {
  navMain: [
    {
      title: "Cluster",
      url: "/nodes",
      icon: <LayoutDashboard className="h-4 w-4" />,
      items: [
        {
          title: "Nodes",
          url: "/nodes",
        },
        {
          title: "Rollouts & Versions",
          url: "/versions",
        },
      ],
    },
    {
      title: "Rings",
      url: "/rings",
      icon: <CircleDot className="h-4 w-4" />,
      items: [
        {
          title: "Ingester",
          url: "/rings/ingester",
        },
        {
          title: "Partition Ingester",
          url: "/rings/partition-ingester",
        },
        {
          title: "Distributor",
          url: "/rings/distributor",
        },
        {
          title: "Pattern Ingester",
          url: "/rings/pattern-ingester",
        },
        {
          title: "Scheduler",
          url: "/rings/scheduler",
        },
        {
          title: "Compactor",
          url: "/rings/compactor",
        },
        {
          title: "Ruler",
          url: "/rings/ruler",
        },
        {
          title: "Index Gateway",
          url: "/rings/index-gateway",
        },
      ],
    },
    {
      title: "Storage",
      url: "/storage",
      icon: <Database className="h-4 w-4" />,
      items: [
        {
          title: "Object Storage",
          url: "/storage/object",
        },
        {
          title: "Data Objects",
          url: "/storage/data",
        },
      ],
    },
    {
      title: "Tenants",
      url: "/tenants",
      icon: <Users className="h-4 w-4" />,
      items: [
        {
          title: "Limits",
          url: "/tenants/limits",
        },
        {
          title: "Labels",
          url: "/tenants/labels",
        },
      ],
    },
    {
      title: "Rules",
      url: "/rules",
      icon: <GaugeCircle className="h-4 w-4" />,
      items: [],
    },
  ],
};

// Custom wrapper for SidebarRail to handle theme-specific styling
function CustomSidebarRail(props: React.ComponentProps<typeof SidebarRail>) {
  return (
    <SidebarRail
      {...props}
      className={cn(
        "after:bg-border/40 hover:after:bg-border",
        "hover:bg-muted/50",
        props.className
      )}
    />
  );
}

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const basename = getBasename();
  const location = useLocation();
  const currentPath = location.pathname.replace(basename, "/");
  const [openSections, setOpenSections] = React.useState<
    Record<string, boolean>
  >(
    data.navMain.reduce(
      (acc, item) => ({
        ...acc,
        [item.title]: true,
      }),
      {}
    )
  );

  const isActive = (url: string) => {
    if (url === "/") {
      return currentPath === "/";
    }
    return currentPath.startsWith(url);
  };

  const toggleSection = (title: string) => {
    setOpenSections((prev) => ({
      ...prev,
      [title]: !prev[title],
    }));
  };

  return (
    <Sidebar {...props}>
      <SidebarHeader className="py-4">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <div className="flex items-center gap-3 px-6 py-4">
                <img
                  src="https://grafana.com/media/docs/loki/logo-grafana-loki.png"
                  alt="Loki Logo"
                  className="h-7 w-7"
                />
                <div className="flex flex-col gap-0.5">
                  <span className="text-sm font-semibold leading-none">
                    Grafana Loki
                  </span>
                  <VersionDisplay />
                </div>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <ScrollArea className="flex-1">
        <SidebarContent>
          <SidebarGroup>
            <SidebarMenu>
              {data.navMain.map((item) => (
                <React.Fragment key={item.title}>
                  <SidebarMenuItem>
                    <SidebarMenuButton
                      asChild
                      isActive={isActive(item.url)}
                      onClick={() => toggleSection(item.title)}
                    >
                      <div className="flex items-center justify-between font-medium">
                        <div className="flex items-center gap-2">
                          {item.icon}
                          {item.title}
                        </div>
                        {item.items?.length > 0 && (
                          <ChevronDown
                            className={cn(
                              "h-4 w-4 transition-transform duration-200",
                              openSections[item.title]
                                ? "rotate-0"
                                : "-rotate-90"
                            )}
                          />
                        )}
                      </div>
                    </SidebarMenuButton>
                    {item.items?.length > 0 && openSections[item.title] && (
                      <SidebarMenuSub>
                        {item.items.map((subItem) => (
                          <SidebarMenuSubItem key={subItem.title}>
                            <SidebarMenuSubButton
                              asChild
                              isActive={isActive(subItem.url)}
                            >
                              <a
                                href={
                                  subItem.url === "#"
                                    ? "#"
                                    : `${basename}${subItem.url.slice(1)}`
                                }
                              >
                                {subItem.title}
                              </a>
                            </SidebarMenuSubButton>
                          </SidebarMenuSubItem>
                        ))}
                      </SidebarMenuSub>
                    )}
                  </SidebarMenuItem>
                </React.Fragment>
              ))}
            </SidebarMenu>
          </SidebarGroup>
        </SidebarContent>
      </ScrollArea>
      <CustomSidebarRail />
    </Sidebar>
  );
}
