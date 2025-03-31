import * as React from "react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { getBasename } from "../util";
import { VersionDisplay } from "@/components/version-display";
import { useLocation, Link } from "react-router-dom";
import {
  ChevronDown,
  CircleDot,
  Database,
  GaugeCircle,
  LayoutDashboard,
  Users,
  BookOpen,
} from "lucide-react";
import { useCluster } from "@/contexts/use-cluster";
import { getAvailableRings } from "@/lib/ring-utils";

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

interface NavItem {
  title: string;
  url: string;
  icon?: React.ReactNode;
  items?: Array<{ title: string; url: string }>;
}

interface Member {
  services?: Array<{ service: string }>;
}

interface ClusterData {
  members?: Record<string, Member>;
}

function useNavItems(
  cluster: ClusterData | null,
  baseItems: NavItem[]
): NavItem[] {
  const [navItems, setNavItems] = React.useState<NavItem[]>(baseItems);

  React.useEffect(() => {
    if (!cluster?.members) return;

    // Update nav items based on available services
    const newItems = baseItems.map((item) => {
      if (item.title === "Rings" && cluster.members) {
        return {
          ...item,
          items: getAvailableRings(cluster.members),
        };
      }
      return item;
    });

    setNavItems(newItems);
  }, [cluster?.members, baseItems]);

  return navItems;
}

const baseNavItems: NavItem[] = [
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
    items: [],
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
        url: "/storage/dataobj",
      },
    ],
  },
  {
    title: "Tenants",
    url: "/tenants",
    icon: <Users className="h-4 w-4" />,
    items: [
      {
        title: "Analyze Labels",
        url: "/tenants/analyze-labels",
      },
      {
        title: "Deletes",
        url: "/tenants/deletes",
      },
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
  {
    title: "Documentation",
    url: "https://grafana.com/docs/loki/latest/",
    icon: <BookOpen className="h-4 w-4" />,
    items: [],
  },
];

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

const OPEN_SECTIONS_KEY = "loki-sidebar-open-sections";

interface NavItemProps {
  item: NavItem;
  isOpen: boolean;
  isActive: (url: string) => boolean;
  onToggle: (title: string) => void;
}

const NavItemComponent = React.memo(function NavItemComponent({
  item,
  isOpen,
  isActive,
  onToggle,
}: NavItemProps) {
  return (
    <SidebarMenuItem>
      <SidebarMenuButton
        asChild
        isActive={isActive(item.url)}
        onClick={() => onToggle(item.title)}
      >
        <div className="flex items-center justify-between font-medium">
          <div className="flex items-center gap-2">
            {item.icon}
            <Link
              to={`${item.url}`}
              target={item.url.includes("http") ? "_blank" : "_self"}
            >
              {item.title}
            </Link>
          </div>
          {item.items && item.items.length > 0 && (
            <ChevronDown
              className={cn(
                "h-4 w-4 transition-transform duration-200",
                isOpen ? "rotate-0" : "-rotate-90"
              )}
            />
          )}
        </div>
      </SidebarMenuButton>
      {item.items && item.items.length > 0 && isOpen && (
        <SidebarMenuSub>
          {item.items.map((subItem) => (
            <SidebarMenuSubItem key={subItem.title}>
              <SidebarMenuSubButton asChild isActive={isActive(subItem.url)}>
                <Link to={`${subItem.url}`}>{subItem.title}</Link>
              </SidebarMenuSubButton>
            </SidebarMenuSubItem>
          ))}
        </SidebarMenuSub>
      )}
    </SidebarMenuItem>
  );
});

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const basename = getBasename();
  const location = useLocation();
  const { cluster } = useCluster();
  const currentPath = location.pathname.replace(basename, "/");

  // Initialize state from localStorage or default to all sections open
  const [openSections, setOpenSections] = React.useState<
    Record<string, boolean>
  >(() => {
    const stored = localStorage.getItem(OPEN_SECTIONS_KEY);
    if (stored) {
      try {
        return JSON.parse(stored);
      } catch {
        return baseNavItems.reduce(
          (acc, item) => ({
            ...acc,
            [item.title]: true,
          }),
          {}
        );
      }
    }
    return baseNavItems.reduce(
      (acc, item) => ({
        ...acc,
        [item.title]: true,
      }),
      {}
    );
  });

  // Use the stable nav items hook
  const navItems = useNavItems(cluster, baseNavItems);

  const isActive = React.useCallback(
    (url: string) => {
      if (url === "/") {
        return currentPath === "/";
      }
      return currentPath.startsWith(url);
    },
    [currentPath]
  );

  const toggleSection = React.useCallback((title: string) => {
    setOpenSections((prev) => {
      const next = {
        ...prev,
        [title]: !prev[title],
      };
      localStorage.setItem(OPEN_SECTIONS_KEY, JSON.stringify(next));
      return next;
    });
  }, []);

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
              {navItems.map((item) => (
                <React.Fragment key={item.title}>
                  <NavItemComponent
                    item={item}
                    isOpen={openSections[item.title]}
                    isActive={isActive}
                    onToggle={toggleSection}
                  />
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
