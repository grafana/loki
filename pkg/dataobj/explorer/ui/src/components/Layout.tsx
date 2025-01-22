import React, { useEffect, useState } from "react";
import { DarkModeToggle } from "./DarkModeToggle";
import { Breadcrumb } from "./Breadcrumb";

interface LayoutProps {
  children: React.ReactNode;
  breadcrumbParts?: string[];
  onBreadcrumbNavigate?: (path: string) => void;
  isLastBreadcrumbClickable?: boolean;
}

export const Layout: React.FC<LayoutProps> = ({
  children,
  breadcrumbParts = [],
  onBreadcrumbNavigate,
  isLastBreadcrumbClickable = false,
}) => {
  const [isDarkMode, setIsDarkMode] = useState(() => {
    const savedTheme = localStorage.getItem("theme");
    const systemPreference = window.matchMedia(
      "(prefers-color-scheme: dark)"
    ).matches;
    return savedTheme ? savedTheme === "dark" : systemPreference;
  });

  useEffect(() => {
    document.documentElement.classList.toggle("dark", isDarkMode);
    localStorage.setItem("theme", isDarkMode ? "dark" : "light");
  }, [isDarkMode]);

  return (
    <div
      className={`min-h-screen ${
        isDarkMode
          ? "dark:bg-gray-900 dark:text-gray-200"
          : "bg-white text-black"
      }`}
    >
      <div className="container mx-auto px-4 py-8">
        <div className="absolute top-4 right-4">
          <DarkModeToggle
            isDarkMode={isDarkMode}
            onToggle={() => setIsDarkMode(!isDarkMode)}
          />
        </div>
        {breadcrumbParts.length > 0 && onBreadcrumbNavigate && (
          <Breadcrumb
            parts={breadcrumbParts}
            onNavigate={onBreadcrumbNavigate}
            isLastPartClickable={isLastBreadcrumbClickable}
          />
        )}
        {children}
      </div>
    </div>
  );
};
