// @ts-check
/**
 * lib/mlet-urn.js — client-side `urn:mlet:` construction (D-25),
 * mirroring core.URNMlet exactly: BLAKE3-256 multihash (0x1E 0x20 ‖
 * digest), multibase base32-lower unpadded with the 'b' prefix. The
 * composer's file door hashes BEFORE upload (D-135: the have-check
 * is an address question), over the vendored pure-JS blake3
 * (lib/vendor/noble — no wasm, no CSP concession; D-244).
 */

import { blake3 } from './vendor/noble/blake3.js';

const B32 = 'abcdefghijklmnopqrstuvwxyz234567';

/**
 * RFC 4648 base32, lowercase, no padding.
 * @param {Uint8Array} bytes
 * @returns {string}
 */
export function base32Lower(bytes) {
  let out = '';
  let buffer = 0;
  let bits = 0;
  for (const b of bytes) {
    buffer = (buffer << 8) | b;
    bits += 8;
    while (bits >= 5) {
      out += B32[(buffer >>> (bits - 5)) & 31];
      bits -= 5;
    }
  }
  if (bits > 0) out += B32[(buffer << (5 - bits)) & 31];
  return out;
}

/**
 * The content address of raw bytes.
 * @param {Uint8Array} data
 * @returns {string}
 */
export function urnMlet(data) {
  const digest = blake3(data);
  const multihash = new Uint8Array(2 + digest.length);
  multihash[0] = 0x1e;
  multihash[1] = 0x20;
  multihash.set(digest, 2);
  return 'urn:mlet:b' + base32Lower(multihash);
}

/**
 * Hash a File/Blob without holding it whole in memory: blake3
 * streams through fixed slices.
 * @param {Blob} blob
 * @param {(done: number, total: number) => void} [onProgress]
 * @returns {Promise<string>}
 */
export async function urnMletOfBlob(blob, onProgress) {
  const hasher = blake3.create({});
  const step = 4 * 1024 * 1024;
  for (let off = 0; off < blob.size; off += step) {
    const slice = new Uint8Array(await blob.slice(off, off + step).arrayBuffer());
    hasher.update(slice);
    if (onProgress) onProgress(Math.min(off + step, blob.size), blob.size);
  }
  const digest = hasher.digest();
  const multihash = new Uint8Array(2 + digest.length);
  multihash[0] = 0x1e;
  multihash[1] = 0x20;
  multihash.set(digest, 2);
  return 'urn:mlet:b' + base32Lower(multihash);
}
