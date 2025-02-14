/**
 * Extracts the basename from the current URL by matching everything up to and including /ui/
 * @returns The basename string, defaults to "/ui/" if no match is found
 */
export function getBasename(): string {
  const pathname = window.location.pathname;
  const match = pathname.match(/(.*\/ui\/)/);
  return match?.[1] || "/ui/";
}
