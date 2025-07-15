import { GitHubLink } from "./github-link";
import { ThemeToggle } from "../features/theme";

export function HeaderActions() {
  return (
    <div className="flex items-center gap-2">
      <GitHubLink />
      <ThemeToggle />
    </div>
  );
}
