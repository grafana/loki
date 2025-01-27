import React, { createContext, useContext } from "react";

interface BasenameContextType {
  basename: string;
}

const BasenameContext = createContext<BasenameContextType | undefined>(
  undefined
);

export function useBasename() {
  const context = useContext(BasenameContext);
  if (context === undefined) {
    throw new Error("useBasename must be used within a BasenameProvider");
  }
  return context.basename;
}

export function BasenameProvider({
  basename,
  children,
}: {
  basename: string;
  children: React.ReactNode;
}) {
  return (
    <BasenameContext.Provider value={{ basename }}>
      {children}
    </BasenameContext.Provider>
  );
}
