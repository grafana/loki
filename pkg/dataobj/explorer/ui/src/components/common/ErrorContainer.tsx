import React from "react";

interface ErrorContainerProps {
  message: string;
  fullScreen?: boolean;
}

export const ErrorContainer: React.FC<ErrorContainerProps> = ({
  message,
  fullScreen = false,
}) => (
  <div
    className={`flex items-center justify-center ${
      fullScreen ? "min-h-screen" : ""
    }`}
  >
    <div className="text-red-500 p-4">Error: {message}</div>
  </div>
);
