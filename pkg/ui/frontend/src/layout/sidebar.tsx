import * as React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";

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
      url: "#",
      items: [
        {
          title: "Nodes",
          url: "/nodes",
        },
        {
          title: "Rollouts",
          url: "/rollouts",
        },
      ],
    },
    {
      title: "Rings",
      url: "#",
      items: [
        {
          title: "Ingester",
          url: "#",
        },
        {
          title: "Partition Ingester",
          url: "#",
        },
        {
          title: "Distributor",
          url: "#",
          isActive: true,
        },
        {
          title: "Pattern Ingester",
          url: "#",
        },
        {
          title: "Scheduler",
          url: "#",
        },
        {
          title: "Compactor",
          url: "#",
        },
        {
          title: "Ruler",
          url: "#",
        },
        {
          title: "Index Gateway",
          url: "#",
        },
      ],
    },
    {
      title: "Storage",
      url: "#",
      items: [
        {
          title: "Data Objects",
          url: "#",
        },
      ],
    },
    {
      title: "Tenants",
      url: "#",
      items: [
        {
          title: "Limits",
          url: "#",
        },
        {
          title: "Labels",
          url: "#",
        },
      ],
    },
    {
      title: "Rules",
      url: "#",
      items: [],
    },
  ],
};

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  return (
    <Sidebar {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <a href="#" className="flex items-center gap-4 px-6 py-2">
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
                    <a href={item.url} className="font-medium">
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
                            <a href={item.url}>{item.title}</a>
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
