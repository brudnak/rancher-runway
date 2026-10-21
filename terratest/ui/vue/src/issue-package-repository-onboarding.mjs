// Each authoring attempt owns one continuation. The controller only reads the
// setup decision; the dialog owns linking and the persistent skip preference.
export function createRepositoryOnboardingController({request, onState = () => {}} = {}) {
  if (typeof request !== 'function') throw new TypeError('An onboarding request function is required.');
  const handled = new Set();
  let active = null, sequence = 0, disposed = false;

  function publish(phase, entry = null, error = '') {
    if (entry) entry.phase = phase;
    onState({phase, context:entry ? {...entry.context} : null, error});
  }
  function finish(accepted) {
    const entry = active;
    if (!entry) return false;
    active = null;
    sequence++;
    if (accepted) handled.add(entry.context.key);
    publish('idle');
    entry.resolve(accepted);
    return true;
  }
  async function read(entry) {
    const ticket = ++sequence;
    const current = () => !disposed && active === entry && sequence === ticket;
    publish('checking', entry);
    if (!current()) return;
    try {
      const result = await request('plan-git-onboarding', {id:entry.context.packageId});
      if (!current()) return;
      if (typeof result?.required !== 'boolean') throw new Error('The repository setup response was incomplete. Try again or skip for now.');
      if (result.required) publish('onboarding', entry);
      else finish(true);
    } catch (error) {
      if (current()) publish('error', entry, String(error?.message || error || 'Repository setup could not be checked.'));
    }
  }
  function begin({key, packageId = '', contextTitle = ''} = {}) {
    if (disposed) return Promise.resolve(false);
    if (typeof key !== 'string' || !key) return Promise.reject(new TypeError('An authoring attempt key is required.'));
    if (active?.context.key === key) return active.promise;
    finish(false);
    if (handled.has(key)) return Promise.resolve(true);
    let resolve;
    const promise = new Promise(done => { resolve = done; });
    const entry = {context:{key, packageId:String(packageId || ''), contextTitle:String(contextTitle || '')}, promise, resolve, phase:'checking'};
    active = entry;
    void read(entry);
    return promise;
  }
  function retry() {
    if (disposed || !active) return Promise.resolve(false);
    const entry = active;
    if (entry.phase !== 'checking') void read(entry);
    return entry.promise;
  }
  function complete() {
    if (disposed) return false;
    return finish(true);
  }
  function cancel() {
    return finish(false);
  }
  function allow(key) {
    if (!disposed && typeof key === 'string' && key) handled.add(key);
  }
  function dispose() {
    if (disposed) return;
    disposed = true;
    cancel();
    handled.clear();
  }
  return {begin, retry, complete, cancel, allow, dispose};
}
