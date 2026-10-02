// Durable writing recovery is separate from the explicit Save plan checkpoint.
// Each package has one serialized queue and a server-owned compare-and-swap
// revision. A late autosave therefore cannot resurrect a discarded draft.
const copy = value => JSON.parse(JSON.stringify(value));
const isConflict = error => error?.status === 409 || /changed in another view|writing draft changed/i.test(String(error?.message || error));

export function createPackageDraftController({request, onState = () => {}, onSaved = () => {}, delay = 400, timers = globalThis} = {}) {
  if (typeof request !== 'function') throw new TypeError('A draft request function is required.');
  const entries = new Map();
  let disposed = false;
  function entryFor(id) {
    if (!entries.has(id)) entries.set(id, {id, response:null, revision:'', status:'idle', updatedAt:null, error:null, pending:null, writing:false, timer:null, tail:Promise.resolve(), blocked:null, epoch:0, needsReview:false, forgotten:false});
    return entries.get(id);
  }
  function snapshot(entry) {
    return {status:entry.status, revision:entry.revision, updatedAt:entry.updatedAt, error:entry.error, hasPending:entry.pending !== null || entry.writing, needsReview:entry.needsReview};
  }
  function emit(entry, status, error = null) {
    entry.status = status;
    entry.error = error ? String(error.message || error) : null;
    if (!entry.forgotten) onState(entry.id, snapshot(entry));
  }
  function cancelTimer(entry) {
    if (entry.timer !== null) timers.clearTimeout(entry.timer);
    entry.timer = null;
  }
  function serial(entry, operation) {
    const task = entry.tail.then(operation);
    entry.tail = task.catch(() => {});
    return task;
  }
  function accept(entry, response) {
    if (!response?.draft || typeof response.draft.revision !== 'string') throw new Error('The writing draft response was incomplete. Reload before continuing.');
    entry.response = copy(response);
    entry.revision = response.draft.revision;
    entry.updatedAt = response.draft.updatedAt || null;
    return response;
  }
  function fail(entry, error) {
    entry.blocked = error;
    emit(entry, isConflict(error) ? 'conflict' : 'error', error);
  }
  async function read(entry, force) {
    if (entry.response && !force) return copy(entry.response);
    emit(entry, 'loading');
    try {
      const response = accept(entry, await request('draft-read', {id:entry.id}));
      entry.blocked = null;
      entry.needsReview = force && entry.pending !== null;
      if (entry.needsReview) emit(entry, 'conflict', new Error('Review the recovered and local drafts, then choose which text to keep.'));
      else emit(entry, entry.pending ? 'pending' : (entry.updatedAt ? 'saved' : 'idle'));
      return copy(response);
    } catch (error) {
      fail(entry, error);
      throw error;
    }
  }
  async function drain(entry, epoch) {
    if (epoch !== entry.epoch) return entry.response ? copy(entry.response) : null;
    if (entry.blocked) throw entry.blocked;
    if (entry.needsReview) throw new Error('Review the recovered and local drafts before saving.');
    await read(entry, false);
    while (entry.pending !== null && epoch === entry.epoch) {
      const payload = entry.pending;
      entry.pending = null;
      entry.writing = true;
      emit(entry, 'saving');
      try {
        const response = accept(entry, await request('draft-write', {id:entry.id, draftRevision:entry.revision, draft:payload}));
        entry.writing = false;
        emit(entry, entry.pending ? 'pending' : 'saved');
        if (!entry.forgotten) onSaved(entry.id, copy(response));
      } catch (error) {
        entry.writing = false;
        // Keep the most recent text, including any edits made during the request.
        if (entry.pending === null && epoch === entry.epoch) entry.pending = payload;
        fail(entry, error);
        throw error;
      }
    }
    return copy(entry.response);
  }
  function queue(id, payload) {
    if (disposed) return;
    const entry = entryFor(id);
    entry.pending = copy(payload);
    entry.needsReview = false;
    cancelTimer(entry);
    if (entry.blocked) {
      emit(entry, isConflict(entry.blocked) ? 'conflict' : 'error', entry.blocked);
      return;
    }
    emit(entry, entry.writing ? 'saving' : 'pending');
    entry.timer = timers.setTimeout(() => {
      entry.timer = null;
      if (!disposed) flush(id).catch(() => {});
    }, delay);
  }
  function flush(id) {
    const entry = entryFor(id);
    cancelTimer(entry);
    const epoch = entry.epoch;
    return serial(entry, () => drain(entry, epoch));
  }
  function load(id, {force = false} = {}) {
    const entry = entryFor(id);
    cancelTimer(entry);
    // Force reload does not automatically write pending text: callers first
    // review the other view's work, then explicitly queue the chosen draft.
    return serial(entry, () => read(entry, force));
  }
  function clear(id) {
    const entry = entryFor(id);
    cancelTimer(entry);
    entry.pending = null;
    entry.epoch += 1;
    entry.needsReview = false;
    return serial(entry, async () => {
      if (entry.blocked) throw entry.blocked;
      await read(entry, false);
      entry.writing = true;
      emit(entry, 'saving');
      try {
        const response = accept(entry, await request('draft-clear', {id, draftRevision:entry.revision}));
        entry.writing = false;
        emit(entry, entry.pending ? 'pending' : 'saved');
        if (!entry.forgotten) onSaved(entry.id, copy(response));
        return copy(response);
      } catch (error) {
        entry.writing = false;
        fail(entry, error);
        throw error;
      }
    });
  }
  return {
    load, queue, flush, clear,
    write(id, payload) { queue(id, payload); return flush(id); },
    state(id) { return snapshot(entryFor(id)); },
    forget(id) {
      const entry = entries.get(id);
      if (!entry) return;
      cancelTimer(entry);
      entry.pending = null;
      entry.epoch += 1;
      entry.forgotten = true;
      entries.delete(id);
    },
    dispose() { disposed = true; for (const entry of entries.values()) cancelTimer(entry); },
  };
}

export function packageDraftHasWriting(draft) {
  return Boolean(draft?.plan || draft?.observations?.length);
}

export function packageDraftObservationKey(packageId, sessionId, caseId) {
  return `${packageId}/${sessionId}/${caseId}`;
}
