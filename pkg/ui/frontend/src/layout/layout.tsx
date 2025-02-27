import React from "react";
import { HeaderActions } from "./header-actions";
import { BreadcrumbNav } from "@/components/shared/breadcrumb-nav";
import { AppSidebar } from "./sidebar";
import {
  SidebarInset,
  SidebarProvider,
  SidebarTrigger,
} from "../components/ui/sidebar";
import { Separator } from "../components/ui/separator";
import { ScrollToTop } from "../components/ui/scroll-to-top";
import { Toaster } from "@/components/ui/toaster";

interface AppLayoutProps {
  children: React.ReactNode;
}

export function AppLayout({ children }: AppLayoutProps) {
  return (
    <div className="flex min-h-screen">
      <SidebarProvider>
        <AppSidebar />
        <SidebarInset>
          <header className="flex h-16 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger />
            <Separator orientation="vertical" className="mr-2 h-4" />
            <BreadcrumbNav />
            <div className="ml-auto px-4">
              <HeaderActions />
            </div>
          </header>
          <main className="flex flex-1 flex-col">{children}</main>
          <Toaster />
          <ScrollToTop />
        </SidebarInset>
      </SidebarProvider>
    </div>
  );
}
