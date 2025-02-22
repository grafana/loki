import { absolutePath } from "@/util";
import { useState, useEffect } from "react";

interface LogLevelResponse {
  status: string;
  message: string;
}

interface UseLogLevelResult {
  logLevel: string;
  isLoading: boolean;
  error: string | null;
  success: boolean;
  setLogLevel: (level: string) => Promise<void>;
}

export function useLogLevel(nodeName: string | undefined): UseLogLevelResult {
  const [logLevel, setLogLevelState] = useState<string>("info");
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState(false);

  const parseLogLevel = (response: LogLevelResponse): string => {
    const match = response.message.match(/Current log level is (\w+)/);
    return match?.[1] || "info";
  };

  useEffect(() => {
    async function fetchLogLevel() {
      if (!nodeName) return;

      setIsLoading(true);
      setError(null);

      try {
        const res = await fetch(
          absolutePath(`/api/v1/proxy/${nodeName}/log_level`)
        );
        if (!res.ok)
          throw new Error(`Failed to fetch log level: ${res.statusText}`);
        const data: LogLevelResponse = await res.json();
        setLogLevelState(parseLogLevel(data));
      } catch (err) {
        setError(
          err instanceof Error ? err.message : "Failed to fetch log level"
        );
      } finally {
        setIsLoading(false);
      }
    }

    fetchLogLevel();
  }, [nodeName]);

  const setLogLevel = async (level: string) => {
    if (!nodeName) return;

    setIsLoading(true);
    setError(null);
    setSuccess(false);

    try {
      const res = await fetch(
        absolutePath(`/api/v1/proxy/${nodeName}/log_level?log_level=${level}`),
        { method: "POST" }
      );
      if (!res.ok)
        throw new Error(`Failed to update log level: ${res.statusText}`);
      const data: LogLevelResponse = await res.json();

      if (data.status === "success" && data.message.includes(level)) {
        setLogLevelState(level);
        setSuccess(true);
        // Reset success state after 3 seconds
        setTimeout(() => setSuccess(false), 3000);
      } else {
        throw new Error("Failed to update log level: Unexpected response");
      }
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to update log level"
      );
    } finally {
      setIsLoading(false);
    }
  };

  return {
    logLevel,
    isLoading,
    error,
    success,
    setLogLevel,
  };
}
