import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { Button } from "@/components/ui/button";
import { useVersionInfo } from "@/hooks/use-version-info";
import { Loader2, Copy, Check } from "lucide-react";
import { cn } from "@/lib/utils";
import { useState } from "react";

export function VersionDisplay() {
  const { mostCommonVersion, versionInfos, isLoading } = useVersionInfo();
  const [isOpen, setIsOpen] = useState(false);
  const [hasCopied, setHasCopied] = useState(false);

  const copyToClipboard = () => {
    const text = versionInfos
      .map(
        ({ version, info }) => `Version: ${version}
Revision: ${info.revision}
Branch: ${info.branch}
Build User: ${info.buildUser}
Build Date: ${info.buildDate}
Go Version: ${info.goVersion}
`
      )
      .join("\n");

    navigator.clipboard.writeText(text).then(() => {
      setHasCopied(true);
      setTimeout(() => setHasCopied(false), 2000);
    });
  };

  return (
    <HoverCard open={isOpen} onOpenChange={setIsOpen}>
      <HoverCardTrigger asChild>
        <span className="text-sm text-muted-foreground flex items-center gap-1">
          <button
            onClick={() => setIsOpen(!isOpen)}
            className={cn(
              "transition-opacity duration-200 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded px-1 -mx-1",
              {
                "opacity-0": isLoading,
                "opacity-100": !isLoading,
              }
            )}
          >
            {mostCommonVersion}
          </button>
          {isLoading && (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              Loading...
            </>
          )}
        </span>
      </HoverCardTrigger>
      <HoverCardContent side="bottom" align="start" className="w-[400px]">
        <div className="p-2">
          <div className="flex items-center justify-between mb-2">
            <div className="font-semibold">Build Information</div>
            {!isLoading && versionInfos.length > 0 && (
              <Button
                variant="ghost"
                size="sm"
                onClick={copyToClipboard}
                className="h-8 px-2"
              >
                {hasCopied ? (
                  <>
                    <Check className="h-4 w-4 mr-1" />
                    Copied
                  </>
                ) : (
                  <>
                    <Copy className="h-4 w-4 mr-1" />
                    Copy
                  </>
                )}
              </Button>
            )}
          </div>
          <div
            className={cn("transition-opacity duration-200", {
              "opacity-0": isLoading,
              "opacity-100": !isLoading,
            })}
          >
            {versionInfos.length > 0 ? (
              versionInfos.map(({ version, info }) => (
                <div key={version} className="mb-2 last:mb-0">
                  <div className="font-semibold">{version}</div>
                  <div className="text-sm">
                    <div>Revision: {info.revision}</div>
                    <div>Branch: {info.branch}</div>
                    <div>Build User: {info.buildUser}</div>
                    <div>Build Date: {info.buildDate}</div>
                    <div>Go Version: {info.goVersion}</div>
                  </div>
                </div>
              ))
            ) : (
              <div className="text-sm text-muted-foreground">
                No build information available
              </div>
            )}
          </div>
          {isLoading && (
            <div className="flex items-center gap-2">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span>Loading build information...</span>
            </div>
          )}
        </div>
      </HoverCardContent>
    </HoverCard>
  );
}
