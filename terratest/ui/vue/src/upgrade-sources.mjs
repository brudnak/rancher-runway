// A registry failure must not erase usable candidates from other sources.
export async function discoverHeadSources(sources, request) {
  const results = await Promise.allSettled(sources.map(source => request('heads', {
    registry: source.registry, distribution: source.distribution,
  })));
  return sources.map((source, index) => {
    const result = results[index];
    return { ...source, images: result.status === 'fulfilled' ? result.value.images || [] : [],
      error: result.status === 'rejected' ? result.reason?.message || String(result.reason) : '' };
  });
}
export const imageRegistry = image => String(image || '').split('/')[0];
export function imageCommitTag(image) {
  return String(image || '').split(':').at(-1).match(/-([0-9a-f]{7,40})-head$/i)?.[1] || '';
}
