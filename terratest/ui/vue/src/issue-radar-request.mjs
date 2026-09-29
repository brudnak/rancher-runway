import { readJSON } from './read-json.mjs';

// Keep both user cancellation and the body-read deadline active until JSON is parsed.
export async function readRadarJSON(fetchResponse, controller, label, timeoutMs = 185000) {
  let unlink = () => {};
  try {
    return await readJSON(deadline => {
      const abort = () => controller.abort();
      deadline.addEventListener('abort', abort, { once: true });
      unlink = () => deadline.removeEventListener('abort', abort);
      return fetchResponse(controller.signal);
    }, { label, timeoutMs });
  } finally { unlink(); }
}
