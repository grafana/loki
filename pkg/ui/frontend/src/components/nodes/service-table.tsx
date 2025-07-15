import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ScrollArea } from "@/components/ui/scroll-area";

interface Service {
  service: string;
  status: string;
}

interface ServiceTableProps {
  services: Service[];
}

const getStatusColor = (status: string) => {
  switch (status) {
    case "Running":
      return "text-green-600 dark:text-green-400";
    case "Starting":
      return "text-yellow-600 dark:text-yellow-400";
    case "Failed":
      return "text-red-600 dark:text-red-400";
    case "New":
      return "text-blue-600 dark:text-blue-400";
    case "Terminated":
      return "text-gray-600 dark:text-gray-400";
    default:
      return "text-gray-600 dark:text-gray-400";
  }
};

export function ServiceTable({ services }: ServiceTableProps) {
  return (
    <ScrollArea className="h-[180px] rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Service</TableHead>
            <TableHead className="text-right">Status</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {services.map((service) => (
            <TableRow key={service.service} className="hover:bg-muted/50">
              <TableCell className="font-medium">{service.service}</TableCell>
              <TableCell
                className={`text-right ${getStatusColor(
                  service.status
                )} font-medium`}
              >
                {service.status}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </ScrollArea>
  );
}
