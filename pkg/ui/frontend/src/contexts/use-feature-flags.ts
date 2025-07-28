import { useContext } from 'react';
import { FeatureFlagsContext } from './feature-flags';

export function useFeatureFlags() {
  return useContext(FeatureFlagsContext);
}