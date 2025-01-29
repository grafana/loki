import React from "react";

import { ThemeToggle } from "../theme-toggle";

interface AppLayoutProps {
  children: React.ReactNode;
}

export function AppLayout({ children }: AppLayoutProps) {
  return (
    <div className="flex min-h-screen">
      <div className="flex-1">
        <header className="sticky top-0 z-50 border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900">
          <div className="flex h-14 items-center justify-end px-4">
            <ThemeToggle />
          </div>
        </header>
        <main className="flex-1">{children}</main>
      </div>
    </div>
  );
}
