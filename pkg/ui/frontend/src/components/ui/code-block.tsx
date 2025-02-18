import { CodeBlock as ReactCodeBlock } from "react-code-block";
import { Button } from "./button";
import { Copy, Check, ChevronDown, ChevronUp } from "lucide-react";
import { cn } from "@/lib/utils";
import { useState } from "react";
import { useTheme } from "next-themes";
import type { PrismTheme } from "prism-react-renderer";

interface CodeBlockProps {
  code: string;
  language?: string;
  fileName?: string;
  className?: string;
  maxLines?: number;
}

const lightTheme: PrismTheme = {
  plain: {
    color: "var(--foreground)",
    backgroundColor: "var(--muted)",
  },
  styles: [
    {
      types: ["comment"],
      style: {
        color: "#6e7781",
        fontStyle: "italic" as const,
      },
    },
    {
      types: ["keyword", "selector", "changed"],
      style: {
        color: "#cf222e",
      },
    },
    {
      types: ["constant", "number", "builtin"],
      style: {
        color: "#0550ae",
      },
    },
    {
      types: ["string", "attr-value"],
      style: {
        color: "#0a3069",
      },
    },
    {
      types: ["function", "attr-name"],
      style: {
        color: "#8250df",
      },
    },
    {
      types: ["tag", "operator"],
      style: {
        color: "#116329",
      },
    },
    {
      types: ["variable", "property"],
      style: {
        color: "#953800",
      },
    },
    {
      types: ["punctuation"],
      style: {
        color: "#24292f",
      },
    },
  ],
};

const darkTheme: PrismTheme = {
  plain: {
    color: "var(--foreground)",
    backgroundColor: "var(--muted)",
  },
  styles: [
    {
      types: ["comment"],
      style: {
        color: "#8b949e",
        fontStyle: "italic" as const,
      },
    },
    {
      types: ["keyword", "selector", "changed"],
      style: {
        color: "#ff7b72",
      },
    },
    {
      types: ["constant", "number", "builtin"],
      style: {
        color: "#79c0ff",
      },
    },
    {
      types: ["string", "attr-value"],
      style: {
        color: "#a5d6ff",
      },
    },
    {
      types: ["function", "attr-name"],
      style: {
        color: "#d2a8ff",
      },
    },
    {
      types: ["tag", "operator"],
      style: {
        color: "#7ee787",
      },
    },
    {
      types: ["variable", "property"],
      style: {
        color: "#ffa657",
      },
    },
    {
      types: ["punctuation"],
      style: {
        color: "#c9d1d9",
      },
    },
  ],
};

export function CodeBlock({
  code,
  language = "typescript",
  fileName,
  className,
  maxLines = 200,
}: CodeBlockProps) {
  const [copied, setCopied] = useState(false);
  const [isExpanded, setIsExpanded] = useState(false);
  const { theme } = useTheme();

  const onCopy = async () => {
    await navigator.clipboard.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const lines = code.split("\n");
  const shouldTruncate = lines.length > maxLines;
  const displayedCode = isExpanded ? code : lines.slice(0, maxLines).join("\n");

  return (
    <div className={cn("relative group rounded-lg overflow-hidden", className)}>
      {fileName && (
        <div className="flex items-center justify-between px-4 py-2 border-b bg-muted/50">
          <div className="text-sm text-muted-foreground">{fileName}</div>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
            onClick={onCopy}
          >
            {copied ? (
              <Check className="h-4 w-4" />
            ) : (
              <Copy className="h-4 w-4" />
            )}
            <span className="sr-only">Copy code</span>
          </Button>
        </div>
      )}
      <ReactCodeBlock
        code={displayedCode}
        language={language}
        theme={theme === "dark" ? darkTheme : lightTheme}
      >
        <ReactCodeBlock.Code className="bg-muted/50 p-4 text-sm whitespace-pre-wrap break-words">
          <ReactCodeBlock.LineContent className="max-w-full">
            <ReactCodeBlock.Token />
          </ReactCodeBlock.LineContent>
        </ReactCodeBlock.Code>
      </ReactCodeBlock>
      {shouldTruncate && (
        <div className="flex justify-center p-2 border-t bg-muted/50">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setIsExpanded(!isExpanded)}
            className="flex items-center gap-2"
          >
            {isExpanded ? (
              <>
                Show Less <ChevronUp className="h-4 w-4" />
              </>
            ) : (
              <>
                Show More ({lines.length - maxLines} more lines){" "}
                <ChevronDown className="h-4 w-4" />
              </>
            )}
          </Button>
        </div>
      )}
    </div>
  );
}
