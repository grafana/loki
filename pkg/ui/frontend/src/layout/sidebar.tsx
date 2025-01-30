import * as React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { getBasename } from "../util";

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

// This is sample data.
const data = {
  navMain: [
    {
      title: "Cluster",
      url: "/nodes",
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
      // loop through static components supporting ring pages.
      // reuse the list for fetching ring details.
      // TODO: add ring details page.
      // TODO: only shows rings pages that are available in the cluster.
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
          isActive: true,
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
      items: [],
    },
  ],
};

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const basename = getBasename();

  return (
    <Sidebar {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <a href={basename} className="flex items-center gap-4 px-6 py-2">
                <img
                  src="https://grafana.com/media/docs/loki/logo-grafana-loki.png"
                  alt="Loki Logo"
                  className="h-10 w-10"
                />
                <div className="flex flex-col justify-center gap-0.5">
                  <span className="text-base font-semibold">Grafana Loki</span>
                  <span className="text-sm text-muted-foreground">v1.0.0</span>
                </div>
              </a>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <ScrollArea className="flex-1">
        <SidebarContent>
          <SidebarGroup>
            <SidebarMenu>
              {data.navMain.map((item) => (
                <SidebarMenuItem key={item.title}>
                  <SidebarMenuButton asChild>
                    <a
                      href={
                        item.url === "#"
                          ? "#"
                          : `${basename}${item.url.slice(1)}`
                      }
                      className="font-medium"
                    >
                      {item.title}
                    </a>
                  </SidebarMenuButton>
                  {item.items?.length ? (
                    <SidebarMenuSub>
                      {item.items.map((item) => (
                        <SidebarMenuSubItem key={item.title}>
                          <SidebarMenuSubButton
                            asChild
                            isActive={item.isActive}
                          >
                            <a
                              href={
                                item.url === "#"
                                  ? "#"
                                  : `${basename}${item.url.slice(1)}`
                              }
                            >
                              {item.title}
                            </a>
                          </SidebarMenuSubButton>
                        </SidebarMenuSubItem>
                      ))}
                    </SidebarMenuSub>
                  ) : null}
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroup>
        </SidebarContent>
      </ScrollArea>
      <SidebarRail />
    </Sidebar>
  );
}
