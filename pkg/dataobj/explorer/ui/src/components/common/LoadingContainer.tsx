import React from "react";

interface LoadingContainerProps {
  fullScreen?: boolean;
}

export const LoadingContainer: React.FC<LoadingContainerProps> = ({
  fullScreen = false,
}) => (
  <div
    className={`flex items-center justify-center ${
      fullScreen ? "min-h-screen" : "min-h-[200px]"
    } dark:bg-gray-900`}
  >
    <div className="animate-spin rounded-full h-8 w-8 border-t-2 border-b-2 border-blue-500 dark:border-blue-400" />
  </div>
);
