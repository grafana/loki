import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Home, RotateCcw } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { cn } from "@/lib/utils";

export function NotFound() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const pathToShow = searchParams.get("path") || window.location.pathname;

  return (
    <div className="flex h-[80vh] items-center justify-center bg-dot-pattern">
      <Card className="w-[450px] overflow-hidden">
        <CardHeader className="text-center pb-0">
          <div className="relative mb-8">
            <div className="absolute inset-0 flex items-center justify-center"></div>
            <div className="relative flex justify-center py-4">
              <div className="bg-white dark:bg-transparent p-2 rounded-full">
                <img
                  src="https://grafana.com/media/docs/loki/logo-grafana-loki.png"
                  alt="Loki Logo"
                  className={cn(
                    "h-24 w-24",
                    "rotate-180 animate-swing hover:animate-shake cursor-pointer transition-all duration-300"
                  )}
                />
              </div>
            </div>
          </div>
          <CardTitle className="text-7xl font-bold bg-gradient-to-r from-primary to-primary/50 bg-clip-text text-transparent">
            404
          </CardTitle>
        </CardHeader>
        <CardContent className="text-center space-y-3 pb-8">
          <h2 className="text-2xl font-semibold tracking-tight">
            Oops! Page Not Found
          </h2>
          <p className="text-muted-foreground">
            Even with our powerful log aggregation, we couldn't find this page
            in any of our streams!
          </p>
          <p className="text-sm text-muted-foreground italic">
            Error: LogQL query returned 0 results for label{" "}
            {`{path="${pathToShow}"}`}
          </p>
        </CardContent>
        <CardFooter className="flex justify-center gap-4 pb-8">
          <Button
            variant="outline"
            onClick={() => navigate(-1)}
            className="gap-2 group"
          >
            <RotateCcw className="h-4 w-4 group-hover:animate-spin" />
            Go Back
          </Button>
          <Button onClick={() => navigate(`/`)} className="gap-2 group">
            <Home className="h-4 w-4 group-hover:animate-bounce" />
            Go Home
          </Button>
        </CardFooter>
      </Card>
      <style>
        {`
          .bg-dot-pattern {
            background-image: radial-gradient(circle at 1px 1px, hsl(var(--muted-foreground) / 0.1) 1px, transparent 0);
            background-size: 32px 32px;
          }
          @keyframes shake {
            0%, 100% { transform: rotate(180deg); }
            25% { transform: rotate(170deg); }
            75% { transform: rotate(190deg); }
          }
          @keyframes swing {
            0%, 100% { transform: rotate(180deg); }
            50% { transform: rotate(190deg); }
          }
          .animate-swing {
            animation: swing 1s ease-in-out infinite;
          }
          .animate-shake {
            animation: shake 0.3s ease-in-out;
          }
        `}
      </style>
    </div>
  );
}
