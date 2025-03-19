import { createContext, useContext } from "react";

export type Theme = "dark" | "light";

export type ThemeProviderProps = {
  children: React.ReactNode;
  defaultTheme?: Theme;
  storageKey?: string;
};

export type ThemeContextType = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
};

export const initialState: ThemeContextType = {
  theme: "light",
  setTheme: () => null,
};

export const ThemeContext = createContext<ThemeContextType>(initialState);

export function useTheme() {
  const context = useContext(ThemeContext);
  if (context === undefined) {
    throw new Error("useTheme must be used within a ThemeProvider");
  }
  return context;
}
