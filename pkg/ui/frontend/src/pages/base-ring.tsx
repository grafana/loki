import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Link } from "react-router-dom";
import { AlertCircle, CircleDot } from "lucide-react";
import { AVAILABLE_RINGS } from "@/hooks/use-ring";
import { RingType } from "@/types/ring";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { PageContainer } from "@/layout/page-container";

interface BaseRingProps {
  error?: string;
  ringName?: RingType;
}

export function BaseRing({ error, ringName }: BaseRingProps) {
  // Show error state if there is an error
  if (error) {
    return (
      <PageContainer>
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      </PageContainer>
    );
  }

  // Show ring selection if no ring is selected
  if (!ringName) {
    return (
      <PageContainer>
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
      </PageContainer>
    );
  }

  return null;
}
