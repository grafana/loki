import { useNavigate, useParams } from "react-router-dom";
import { RingType } from "@/types/ring";
import { RegularRing } from "./regular-ring";
import PartitionRing from "./partition-ring";
import { useCluster } from "@/contexts/use-cluster";
import { BaseRing } from "./base-ring";
import { getAvailableRings } from "@/lib/ring-utils";

export default function Ring() {
  const { ringName } = useParams<{ ringName: RingType }>();
  const navigate = useNavigate();
  const { cluster } = useCluster();

  // If no ring is selected, show the base ring component or redirect to first available ring
  if (!ringName) {
    if (cluster?.members) {
      const availableRings = getAvailableRings(cluster.members);
      if (availableRings.length > 0) {
        // Extract the ring name from the URL
        const firstRingName = availableRings[0].url.split("/").pop();
        navigate(`/rings/${firstRingName}`);
        return null;
      }
    }
    return <BaseRing />;
  }

  // If this is a partition ring, render the partition ring component
  if (ringName === "partition-ingester") {
    return <PartitionRing />;
  }

  // Otherwise, show the regular ring component
  return <RegularRing ringName={ringName} />;
}
