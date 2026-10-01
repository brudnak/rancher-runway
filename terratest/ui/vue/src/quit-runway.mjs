// Native Quit runs Wails' OnBeforeClose checks. Never stop its server first:
// cancellation must leave the workspace usable.
export async function quitRunway({runtime, shutdown, closeBrowser}) {
  if (typeof runtime?.Quit === 'function') {
    await runtime.Quit();
    return 'native';
  }
  await shutdown();
  closeBrowser();
  return 'browser';
}
