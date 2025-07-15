import React from "react";

interface CompressionRatioProps {
  compressed: number;
  uncompressed: number;
  showVisualization?: boolean;
}

export const CompressionRatio: React.FC<CompressionRatioProps> = ({
  compressed,
  uncompressed,
  showVisualization = false,
}) => {
  if (compressed === 0 || uncompressed === 0) {
    return <span className="dark:text-gray-200">-</span>;
  }

  const ratio = uncompressed / compressed;
  const hasCompression = ratio > 1;

  return (
    <div className="flex items-center gap-2">
      <div className="font-medium whitespace-nowrap dark:text-gray-200">
        {ratio.toFixed(1)}x
      </div>
      {showVisualization && hasCompression && (
        <div className="flex-1 h-2.5 bg-gray-100 dark:bg-gray-600 border border-gray-200 dark:border-gray-500 rounded relative">
          <div
            className="absolute inset-y-0 left-0 bg-blue-600 dark:bg-blue-500 rounded"
            style={{ width: `${(compressed / uncompressed) * 100}%` }}
          />
        </div>
      )}
    </div>
  );
};
