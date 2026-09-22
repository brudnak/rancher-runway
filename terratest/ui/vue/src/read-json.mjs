// Keep the deadline active through body parsing, not just response headers.
export async function readJSON(request, { label = "The check", timeoutMs = 30000 } = {}) {
  const controller = new AbortController();
  let timer;
  const deadline = new Promise((_, reject) => {
    timer = setTimeout(() => {
      reject(new Error(`${label} did not respond within ${Math.ceil(timeoutMs / 1000)} seconds. Retry the check.`));
      controller.abort();
    }, timeoutMs);
  });
  try {
    return await Promise.race([
      (async () => (await request(controller.signal)).json())(),
      deadline,
    ]);
  } finally {
    clearTimeout(timer);
  }
}
