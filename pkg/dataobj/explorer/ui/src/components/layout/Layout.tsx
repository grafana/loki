import React, { useEffect, useState } from "react";
import { DarkModeToggle } from "./DarkModeToggle";
import { Breadcrumb } from "./Breadcrumb";
import { ScrollToTopButton } from "./ScrollToTopButton";

interface LayoutProps {
  children: React.ReactNode;
  breadcrumbParts?: string[];
  isLastBreadcrumbClickable?: boolean;
}

export const Layout: React.FC<LayoutProps> = ({
  children,
  breadcrumbParts = [],
  isLastBreadcrumbClickable = true,
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
        <div className="flex justify-between items-center mb-6">
          <Breadcrumb
            parts={breadcrumbParts}
            isLastPartClickable={isLastBreadcrumbClickable}
          />
          <DarkModeToggle
            isDarkMode={isDarkMode}
            onToggle={() => setIsDarkMode(!isDarkMode)}
          />
        </div>
        {children}
        <ScrollToTopButton />
      </div>
    </div>
  );
};
