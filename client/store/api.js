// @ts-check
/**
 * store/api.js — the /api/v1 client honoring the D-170 conventions:
 * X-MLP-Client on every mutation (the CSRF posture), client-minted
 * Idempotency-Key on mutating POSTs (D-169 offline-queue safety),
 * problem+json surfaced as typed errors.
 */

export class ApiError extends Error {
  /**
   * @param {number} status
   * @param {string} code
   * @param {string} detail
   */
  constructor(status, code, detail) {
    super(detail || code);
    this.status = status;
    this.code = code;
  }
}

/**
 * @param {string} method
 * @param {string} path under /api/v1
 * @param {unknown} [body]
 * @returns {Promise<any>}
 */
async function call(method, path, body) {
  /** @type {Record<string, string>} */
  const headers = {};
  const mutation = method !== 'GET' && method !== 'HEAD';
  if (mutation) {
    headers['X-MLP-Client'] = 'mlp-web/0.1';
    headers['Idempotency-Key'] = crypto.randomUUID();
  }
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  const resp = await fetch('/api/v1' + path, {
    method, headers, credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (resp.status === 204) return null;
  const isProblem = (resp.headers.get('Content-Type') ?? '').startsWith('application/problem+json');
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok || isProblem) {
    const code = typeof data.type === 'string' ? data.type.replace('urn:mlp:err:', '') : 'malformed';
    throw new ApiError(resp.status, code, data.detail ?? '');
  }
  return data;
}

export const api = {
  /** @param {string} address @param {string} password */
  login: (address, password) => call('POST', '/auth/password', { address, password }),
  /** @param {string} view @param {string} [cursor] */
  threads: (view, cursor) =>
    call('GET', `/threads?view=${encodeURIComponent(view)}${cursor ? '&cursor=' + encodeURIComponent(cursor) : ''}`),
  /** @param {number} id */
  thread: (id) => call('GET', `/threads/${id}`),
  /** @param {number} id @param {'read'|'done'|'flag'} op @param {boolean} [value] */
  triage: (id, op, value) => call('POST', `/threads/${id}/${op}`, value === undefined ? {} : { value }),
  /** @param {string} token */
  undo: (token) => call('POST', '/undo', { token }),
  /** @param {string} urn */
  accept: (urn) => call('POST', `/o/${encodeURIComponent(urn)}/accept`, {}),
  /** @param {object} doc */
  draftCreate: (doc) => call('POST', '/drafts', doc),
  /** @param {string} id @param {object} doc */
  draftSave: (id, doc) => call('PATCH', `/drafts/${id}`, doc),
  /** @param {string} id */
  draftSend: (id) => call('POST', `/drafts/${id}/send`),
  /** @param {string} urn @param {number} size */
  uploadDeclare: (urn, size) => call('POST', '/uploads', { urn, size }),

  /**
   * The upload door's byte half (D-135): HEAD for the durable
   * checkpoint, PATCH raw chunks from it — the tus resume shape.
   * @param {string} uploadPath the server-issued /uploads/{token} path
   */
  uploadHead: async (uploadPath) => {
    const resp = await fetch('/api/v1' + uploadPath.replace(/^\/api\/v1/, ''), {
      method: 'HEAD', credentials: 'same-origin',
    });
    if (!resp.ok) throw new ApiError(resp.status, 'upload', 'offset check failed');
    return Number(resp.headers.get('Upload-Offset') ?? 0);
  },

  /**
   * @param {string} uploadPath
   * @param {number} offset
   * @param {Uint8Array} chunk
   */
  uploadPatch: async (uploadPath, offset, chunk) => {
    const resp = await fetch('/api/v1' + uploadPath.replace(/^\/api\/v1/, ''), {
      method: 'PATCH', credentials: 'same-origin',
      headers: {
        'X-MLP-Client': 'mlp-web/0.1',
        'Upload-Offset': String(offset),
        'Content-Type': 'application/offset+octet-stream',
      },
      body: chunk,
    });
    if (resp.status !== 204) {
      throw new ApiError(resp.status, 'upload', 'chunk refused at offset ' + offset);
    }
    return Number(resp.headers.get('Upload-Offset') ?? offset + chunk.length);
  },
  deliveries: () => call('GET', '/deliveries'),
  /** @param {number} id */
  delivery: (id) => call('GET', `/deliveries/${id}`),
  /** @param {number} id */
  timeline: (id) => call('GET', `/deliveries/${id}/timeline`),
  media: () => call('GET', '/media'),
  /** @param {string} urn @param {boolean} pin */
  pin: (urn, pin) => call('POST', `/o/${encodeURIComponent(urn)}/${pin ? 'pin' : 'unpin'}`, {}),
  /** @param {string} urn */
  objectDelete: (urn) => call('DELETE', `/o/${encodeURIComponent(urn)}`),
  /** @param {number} id */
  junkRelease: (id) => call('POST', `/threads/${id}/release`, {}),
  /** @param {number} id */
  junkBlock: (id) => call('POST', `/threads/${id}/block`, {}),
};
