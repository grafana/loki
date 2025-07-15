import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { absolutePath } from "@/util";

interface PprofControlsProps {
  nodeName: string;
}

const pprofTypes = [
  {
    name: "allocs",
    description: "A sampling of all past memory allocations",
  },
  {
    name: "block",
    description:
      "Stack traces that led to blocking on synchronization primitives",
  },
  {
    name: "heap",
    description: "A sampling of memory allocations of live objects",
  },
  {
    name: "mutex",
    description: "Stack traces of holders of contended mutexes",
  },
  {
    name: "profile",
    urlSuffix: "?seconds=15",
    description: "CPU profile (15 seconds)",
    displayName: "profile",
  },
  {
    name: "goroutine",
    description: "Stack traces of all current goroutines (debug=1)",
    variants: [
      {
        suffix: "?debug=0",
        label: "Basic",
        description: "Basic goroutine info",
      },
      {
        suffix: "?debug=1",
        label: "Standard",
        description: "Standard goroutine stack traces",
      },
      {
        suffix: "?debug=2",
        label: "Full",
        description: "Full goroutine stack dump with additional info",
      },
    ],
  },
  {
    name: "threadcreate",
    description: "Stack traces that led to the creation of new OS threads",
    urlSuffix: "?debug=1",
    displayName: "threadcreate",
  },
  {
    name: "trace",
    description: "A trace of execution of the current program",
    urlSuffix: "?debug=1",
    displayName: "trace",
  },
];

export function PprofControls({ nodeName }: PprofControlsProps) {
  const downloadPprof = (type: string) => {
    window.open(
      absolutePath(`/api/v1/proxy/${nodeName}/debug/pprof/${type}`),
      "_blank"
    );
  };

  return (
    <div className="flex items-center gap-2">
      <span className="text-sm font-medium">Profiling Tools:</span>
      <div className="flex flex-wrap gap-2">
        {pprofTypes.map((type) => {
          if (type.variants) {
            return type.variants.map((variant) => (
              <Tooltip key={`${type.name}${variant.suffix}`}>
                <TooltipTrigger asChild>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() =>
                      downloadPprof(`${type.name}${variant.suffix}`)
                    }
                  >
                    {`${type.name} (${variant.label})`}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{variant.description}</p>
                </TooltipContent>
              </Tooltip>
            ));
          }

          return (
            <Tooltip key={type.name}>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    downloadPprof(`${type.name}${type.urlSuffix || ""}`)
                  }
                >
                  {type.displayName || type.name}
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                <p>{type.description}</p>
              </TooltipContent>
            </Tooltip>
          );
        })}
      </div>
    </div>
  );
}
