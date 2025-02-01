import { useNavigate, useParams } from "react-router-dom";
import { RingType } from "@/types/ring";
import { RegularRing } from "./regular-ring";
import PartitionRing from "./partition-ring";

export default function Ring() {
  const { ringName } = useParams<{ ringName: RingType }>();
  const navigate = useNavigate();

  // If this is a partition ring, render the partition ring component
  if (ringName === "partition-ingester") {
    return <PartitionRing />;
  }

  // If no ring is selected or there's an error, show the base ring component
  if (!ringName) {
    navigate("/ui/404");
    return null;
  }

  // Otherwise, show the regular ring component
  return <RegularRing ringName={ringName} />;
}
