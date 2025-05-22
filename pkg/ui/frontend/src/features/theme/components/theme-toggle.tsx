import { Moon, Sun } from "lucide-react";
import { useTheme } from "../theme-context";
import { Button } from "../../../components/ui/button";

export function ThemeToggle() {
  const { theme, setTheme } = useTheme();

  const handleThemeChange = () => {
    const newTheme = theme === "light" ? "dark" : "light";
    setTheme(newTheme);
  };

  return (
    <Button
      variant="ghost"
      size="icon"
      className="bg-muted hover:bg-muted-hover rounded-lg"
      onClick={handleThemeChange}
    >
      {theme === "light" ? (
        <Sun className="h-[1.2rem] w-[1.2rem]" />
      ) : (
        <Moon className="h-[1.2rem] w-[1.2rem]" />
      )}
      <span className="sr-only">Toggle theme</span>
    </Button>
  );
}
