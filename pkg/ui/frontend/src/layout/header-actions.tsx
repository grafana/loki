import React from "react";
import { GitHubLink } from "./github-link";
import { ThemeToggle } from "../components/theme-toggle";

export function HeaderActions() {
  return (
    <div className="flex items-center gap-2">
      <GitHubLink />
      <ThemeToggle />
    </div>
  );
}
