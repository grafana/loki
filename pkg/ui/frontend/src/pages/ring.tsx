import { useParams } from "react-router-dom";
import { RingType, RingTypes } from "@/types/ring";
import { RegularRing } from "./regular-ring";
import PartitionRing from "./partition-ring";
import { BaseRing } from "./base-ring";

export default function Ring() {
  const { ringName } = useParams<{ ringName: RingType }>();

  const isValidRing = Object.values(RingTypes).includes(ringName as RingType);

  // If no ring is selected, show the base ring component or redirect to first available ring
  if (!ringName || !isValidRing) {
    return <BaseRing />;
  }

  // If this is a partition ring, render the partition ring component
  if (ringName === RingTypes.PARTITION_INGESTER) {
    return <PartitionRing />;
  }

  // Otherwise, show the regular ring component
  return <RegularRing ringName={ringName} />;
}
