import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Link } from "react-router-dom";
import { CircleDot } from "lucide-react";
import { AVAILABLE_RINGS } from "@/hooks/use-ring";
import { RingType } from "@/types/ring";

interface BaseRingProps {
  error?: string;
  ringName?: RingType;
}

export function BaseRing({ error, ringName }: BaseRingProps) {
  // Show error state if there is an error
  if (error) {
    return (
      <div className="p-4">
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="text-destructive">Error</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-destructive">{error}</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  // Show ring selection if no ring is selected
  if (!ringName) {
    return (
      <div className="container space-y-6 p-6">
        <div className="space-y-6">
          <div className="flex items-center space-x-4">
            <CircleDot className="h-6 w-6" />
            <h1 className="text-2xl font-bold tracking-tight">Rings</h1>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {AVAILABLE_RINGS.map((ring) => (
              <Link key={ring.id} to={`/rings/${ring.id}`}>
                <Card className="hover:bg-muted/50 transition-colors cursor-pointer">
                  <CardHeader>
                    <CardTitle>{ring.title}</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm text-muted-foreground">
                      View and manage {ring.title.toLowerCase()} ring members
                    </p>
                  </CardContent>
                </Card>
              </Link>
            ))}
          </div>
        </div>
      </div>
    );
  }

  return null;
}
