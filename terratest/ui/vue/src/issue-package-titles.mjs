export const MAX_PLAN_CASES = 250;
export const MAX_CASE_TITLE_BYTES = 240;

// Requiring whitespace after the marker preserves version names such as "2.15".
export function parseCaseTitles(value = '') {
  return String(value).split(/\r\n?|\n/).map(line => line.trim()
    .replace(/^(?:\d+[.)]|[-*+•])(?:\s+|$)/, '')
    .replace(/^\[[ xX]\](?:\s+|$)/, '')
    .trim()).filter(Boolean);
}

export function caseTitlesError(titles, existingCount = 0) {
  const remaining = Math.max(0, MAX_PLAN_CASES - existingCount);
  if (titles.length > remaining) {
    return remaining === 0
      ? `This plan already has ${MAX_PLAN_CASES} cases. Remove a case before adding more.`
      : `There is room for ${remaining} more ${remaining === 1 ? 'case' : 'cases'}. Keep this list within the ${MAX_PLAN_CASES}-case plan limit.`;
  }
  const encoder = new TextEncoder();
  for (let index = 0; index < titles.length; index++) {
    const title = titles[index];
    if (!title.trim()) return `Case ${index + 1} needs a title.`;
    if (title.includes('\0')) return `Case ${index + 1} contains an unsupported character. Remove it before continuing.`;
    if (encoder.encode(title).length > MAX_CASE_TITLE_BYTES) {
      return `Case ${index + 1} is too long. Shorten its title to ${MAX_CASE_TITLE_BYTES} bytes or fewer; accented characters and emoji can use more than one byte.`;
    }
  }
  return '';
}
