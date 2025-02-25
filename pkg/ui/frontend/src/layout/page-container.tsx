import { cn } from "@/lib/utils";

interface PageContainerProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode;
  /**
   * Whether to add vertical spacing between children
   * @default true
   */
  spacing?: boolean;
}

export function PageContainer({
  children,
  className,
  spacing = true,
  ...props
}: PageContainerProps) {
  return (
    <div className="container p-6">
      <div className={cn(spacing && "space-y-6", className)} {...props}>
        {children}
      </div>
    </div>
  );
}
