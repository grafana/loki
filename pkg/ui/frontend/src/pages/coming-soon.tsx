import { Construction } from "lucide-react";

export default function ComingSoon() {
  return (
    <div className="flex flex-col items-center justify-center min-h-[80vh] p-4">
      <Construction className="h-16 w-16 text-muted-foreground mb-6" />
      <h1 className="text-4xl font-bold text-center mb-4">Coming Soon</h1>
      <p className="text-lg text-muted-foreground text-center max-w-md">
        We're working hard to bring you this feature. Stay tuned for updates!
      </p>
    </div>
  );
}
