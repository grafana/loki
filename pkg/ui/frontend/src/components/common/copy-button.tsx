import { Button } from "@/components/ui/button";
import { Copy, Check } from "lucide-react";
import { useState } from "react";
import { cn } from "@/lib/utils";

interface CopyButtonProps {
  text: string;
  className?: string;
  onCopy?: () => void;
}

export function CopyButton({ text, className, onCopy }: CopyButtonProps) {
  const [hasCopied, setHasCopied] = useState(false);

  const copyToClipboard = () => {
    navigator.clipboard.writeText(text).then(() => {
      setHasCopied(true);
      onCopy?.();
      setTimeout(() => setHasCopied(false), 2000);
    });
  };

  return (
    <Button
      variant="ghost"
      size="sm"
      onClick={copyToClipboard}
      className={cn("h-8 px-2", className)}
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
  );
}
