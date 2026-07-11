// @ts-check
/**
 * store/store.js — one-way state (S3.9 §2, D-114): components render
 * from the store and mutate only through store methods; per-slice
 * events so components hear only what they render.
 */

/**
 * @typedef {{ id: number, done: boolean, flagged: boolean,
 *   last_activity: string, unread: number,
 *   rollup: { subject?: string, last_author?: string, media_count?: number },
 *   media: Record<string, number> }} ThreadRow
 */

/**
 * @typedef {{ authenticated: boolean, mailboxId: number | null,
 *   inbox: { threads: ThreadRow[], loading: boolean, view: string },
 *   openThread: number | null,
 *   undo: { token: string, label: string } | null }} State
 */

class Store extends EventTarget {
  constructor() {
    super();
    /** @type {State} */
    this.state = {
      authenticated: false,
      mailboxId: null,
      inbox: { threads: [], loading: false, view: 'inbox' },
      openThread: null,
      undo: null,
    };
  }

  /**
   * Patches a slice and notifies its subscribers.
   * @param {'auth' | 'inbox' | 'nav' | 'undo'} slice
   * @param {Partial<State> | Partial<State['inbox']>} patch
   */
  update(slice, patch) {
    if (slice === 'inbox') {
      this.state.inbox = { ...this.state.inbox, .../** @type {Partial<State['inbox']>} */ (patch) };
    } else {
      Object.assign(this.state, patch);
    }
    this.dispatchEvent(new CustomEvent(slice + '-changed'));
  }

  /**
   * Lifecycle-bound subscription (connectedCallback subscribes,
   * disconnectedCallback unsubscribes — D-114).
   * @param {string} slice
   * @param {() => void} fn
   * @returns {() => void} unsubscribe
   */
  subscribe(slice, fn) {
    const name = slice + '-changed';
    this.addEventListener(name, fn);
    return () => this.removeEventListener(name, fn);
  }
}

export const store = new Store();
