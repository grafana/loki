import { useEffect, useState } from "react";
import { Theme, ThemeContext, ThemeProviderProps } from "../theme-context";

export function ThemeProvider({
  children,
  defaultTheme = "light",
  storageKey = "loki-ui-theme",
  ...props
}: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(() => {
    try {
      const storedTheme = localStorage.getItem(storageKey);
      return storedTheme === "dark" || storedTheme === "light"
        ? storedTheme
        : defaultTheme;
    } catch {
      return defaultTheme;
    }
  });

  const handleThemeChange = (newTheme: Theme) => {
    try {
      localStorage.setItem(storageKey, newTheme);
      setTheme(newTheme);
    } catch (error) {
      console.error("Failed to save theme:", error);
    }
  };

  useEffect(() => {
    const root = window.document.documentElement;
    root.classList.remove("light", "dark");
    root.classList.add(theme);
  }, [theme]);

  return (
    <ThemeContext.Provider
      value={{ theme, setTheme: handleThemeChange }}
      {...props}
    >
      {children}
    </ThemeContext.Provider>
  );
}
