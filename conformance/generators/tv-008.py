#!/usr/bin/env python3
"""TV-008: the application/mlp-bao encoding (MEP-003, Annex D).

A rule-generated object; its group and parent chaining values; the
full combined encoding and one boundary-crossing slice, each pinned by
BLAKE3 digest; one corrupted slice with its expected rejection point.

The generator embeds a minimal BLAKE3 core (the public reference
algorithm) because it needs interior chaining values, which hash
libraries do not expose. Two self-checks make the tree construction
trustworthy: the root-finalized top CV must equal the independent
`blake3` library's digest of the content (tree correct), and the
emitted slice must decode-verify through an independent walker in this
file (encoding correct). Run from this directory.
"""
import base64
import json

import blake3  # the independent cross-check

# --- minimal BLAKE3 core (reference algorithm) -----------------------

IV = [0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
      0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19]
MSG_PERM = [2, 6, 3, 10, 7, 0, 4, 13, 1, 11, 12, 5, 9, 14, 15, 8]
CHUNK_START, CHUNK_END, PARENT, ROOT = 1, 2, 4, 8
MASK = 0xFFFFFFFF


def _rr(x, n):
    return ((x >> n) | (x << (32 - n))) & MASK


def _g(s, a, b, c, d, mx, my):
    s[a] = (s[a] + s[b] + mx) & MASK
    s[d] = _rr(s[d] ^ s[a], 16)
    s[c] = (s[c] + s[d]) & MASK
    s[b] = _rr(s[b] ^ s[c], 12)
    s[a] = (s[a] + s[b] + my) & MASK
    s[d] = _rr(s[d] ^ s[a], 8)
    s[c] = (s[c] + s[d]) & MASK
    s[b] = _rr(s[b] ^ s[c], 7)


def compress(cv, block_words, counter, block_len, flags):
    s = list(cv) + IV[:4] + [counter & MASK, (counter >> 32) & MASK,
                             block_len, flags]
    m = list(block_words)
    for r in range(7):
        _g(s, 0, 4, 8, 12, m[0], m[1])
        _g(s, 1, 5, 9, 13, m[2], m[3])
        _g(s, 2, 6, 10, 14, m[4], m[5])
        _g(s, 3, 7, 11, 15, m[6], m[7])
        _g(s, 0, 5, 10, 15, m[8], m[9])
        _g(s, 1, 6, 11, 12, m[10], m[11])
        _g(s, 2, 7, 8, 13, m[12], m[13])
        _g(s, 3, 4, 9, 14, m[14], m[15])
        if r < 6:
            m = [m[i] for i in MSG_PERM]
    return [(s[i] ^ s[i + 8]) & MASK for i in range(8)]


def _words(b):
    b = b + b"\x00" * (64 - len(b))
    return [int.from_bytes(b[i:i + 4], "little") for i in range(0, 64, 4)]


def chunk_cv(data, chunk_counter, root=False):
    """CV of one <=1024-byte chunk at its global counter."""
    blocks = [data[i:i + 64] for i in range(0, len(data), 64)] or [b""]
    cv = IV[:]
    for i, blk in enumerate(blocks):
        flags = (CHUNK_START if i == 0 else 0) | \
                (CHUNK_END if i == len(blocks) - 1 else 0)
        if root and i == len(blocks) - 1:
            flags |= ROOT
        cv = compress(cv, _words(blk), chunk_counter, len(blk), flags)
    return cv


def parent_cv(left, right, root=False):
    block = _words(cv_bytes(left) + cv_bytes(right))
    return compress(IV[:], block, 0, 64, PARENT | (ROOT if root else 0))


def cv_bytes(cv):
    return b"".join(w.to_bytes(4, "little") for w in cv)


def subtree_cv(data, first_chunk, root=False):
    """CV of the subtree over `data`, whose first chunk has the given
    global counter. Standard shape: left = largest power of two of
    chunks strictly below the total."""
    n_chunks = max(1, (len(data) + 1023) // 1024)
    if n_chunks == 1:
        return chunk_cv(data, first_chunk, root)
    left = 1
    while left * 2 < n_chunks:
        left *= 2
    split = left * 1024
    l = subtree_cv(data[:split], first_chunk)
    r = subtree_cv(data[split:], first_chunk + left)
    return parent_cv(l, r, root)


# --- the mlp-bao profile (Annex D) -----------------------------------

GROUP = 16384  # bytes; 16 chunks (D.1)


def groups_of(content):
    return [content[i:i + GROUP] for i in range(0, len(content), GROUP)] \
        or [b""]


def tree(content):
    """Returns (root_cv_rooted, parents, group_cvs). parents is a
    pre-order list of dicts {covers, left_cv, right_cv, cv}."""
    gs = groups_of(content)
    cvs = [subtree_cv(g, (i * GROUP) // 1024,
                      root=(len(gs) == 1))
           for i, g in enumerate(gs)]
    parents = []

    def build(lo, hi, root):
        if hi - lo == 1:
            return cvs[lo]
        left = 1
        while left * 2 < hi - lo:
            left *= 2
        node = {"covers": (lo, hi), "pre_order_index": len(parents)}
        parents.append(node)
        l = build(lo, lo + left, False)
        r = build(lo + left, hi, False)
        node["left_cv"], node["right_cv"] = l, r
        node["cv"] = parent_cv(l, r, root)
        return node["cv"]

    root_cv = build(0, len(gs), True)
    # Re-walk to fix pre-order: parents were appended at descent time,
    # which IS pre-order.
    return root_cv, parents, cvs


def encode(content, rng=None):
    """Combined form; with rng=(offset,length), the slice form (D.4)."""
    gs = groups_of(content)
    _, _, cvs = tree(content)
    out = [len(content).to_bytes(8, "little")]

    def keep(lo, hi):
        if rng is None:
            return True
        a, b = rng[0], rng[0] + rng[1]
        return lo * GROUP < b and a < min(hi * GROUP, len(content) or 1)

    def walk(lo, hi):
        if hi - lo == 1:
            if keep(lo, hi):
                out.append(gs[lo])
            return
        left = 1
        while left * 2 < hi - lo:
            left *= 2
        if not keep(lo, hi):
            return
        # Parent node bytes: CVs of the two children.
        mid = lo + left
        lbytes = cv_bytes(subtree_cv(content[lo * GROUP:mid * GROUP],
                                     (lo * GROUP) // 1024))
        rbytes = cv_bytes(subtree_cv(content[mid * GROUP:hi * GROUP],
                                     (mid * GROUP) // 1024))
        out.append(lbytes + rbytes)
        walk(lo, mid)
        walk(mid, hi)

    walk(0, len(gs))
    return b"".join(out)


def verify_slice(sl, root_cv, content_len, rng):
    """Independent walker: decode-verify a slice; returns the list of
    verified group indices, or raises at the first bad node/group."""
    n_groups = max(1, (content_len + GROUP - 1) // GROUP)
    pos = 8
    if int.from_bytes(sl[:8], "little") != content_len:
        raise ValueError("length header")
    verified = []

    def keep(lo, hi):
        a, b = rng[0], rng[0] + rng[1]
        return lo * GROUP < b and a < min(hi * GROUP, content_len or 1)

    def walk(lo, hi, expect, root):
        nonlocal pos
        if hi - lo == 1:
            if not keep(lo, hi):
                return
            size = min(GROUP, content_len - lo * GROUP) if content_len \
                else 0
            data = sl[pos:pos + size]
            pos += size
            if subtree_cv(data, (lo * GROUP) // 1024, root) != expect:
                raise ValueError(f"bao-verify-failed at group {lo}")
            verified.append(lo)
            return
        if not keep(lo, hi):
            return
        node = sl[pos:pos + 64]
        pos += 64
        l = [int.from_bytes(node[i:i + 4], "little") for i in range(0, 32, 4)]
        r = [int.from_bytes(node[i:i + 4], "little") for i in range(32, 64, 4)]
        if parent_cv(l, r, root) != expect:
            raise ValueError(f"bao-verify-failed at parent covering "
                             f"groups {lo}-{hi - 1}")
        left = 1
        while left * 2 < hi - lo:
            left *= 2
        walk(lo, lo + left, l, False)
        walk(lo + left, hi, r, False)

    walk(0, n_groups, root_cv, True)
    return verified


# --- the vector ------------------------------------------------------

def b32l(b):
    return base64.b32encode(b).decode().lower().rstrip("=")


def urn_mlet(digest32):
    return "urn:mlet:b" + b32l(bytes([0x1E, 0x20]) + digest32)


def hexcv(cv):
    return cv_bytes(cv).hex()


def main():
    length = 50152  # 3 full groups + a 1000-byte tail (4 groups)
    content = bytes((i * 7 + 3) & 0xFF for i in range(length))

    root_cv, parents, group_cvs = tree(content)
    root_digest = cv_bytes(root_cv)
    # Self-check 1: the tree is BLAKE3.
    assert root_digest == blake3.blake3(content).digest(), \
        "tree construction diverges from BLAKE3"

    combined = encode(content)
    assert len(combined) == 8 + 64 * (len(group_cvs) - 1) + length

    rng = (32384, 2000)  # crosses the root's left/right boundary
    sl = encode(content, rng)
    # Self-check 2: the slice decode-verifies groups 1 and 2.
    assert verify_slice(sl, root_cv, length, rng) == [1, 2]

    # Corrupt inside group 2's bytes: header 8 + root node 64 +
    # left node 64 + group-1 bytes 16384 + right node 64 + 100.
    corrupt_at = 8 + 64 + 64 + 16384 + 64 + 100
    bad = bytearray(sl)
    bad[corrupt_at] ^= 0x01
    try:
        verify_slice(bytes(bad), root_cv, length, rng)
        raise SystemExit("corrupted slice verified — generator broken")
    except ValueError as e:
        assert "group 2" in str(e), e

    vector = {
        "vector": "TV-008",
        "title": "The application/mlp-bao encoding (MEP-003, Annex D)",
        "spec": "MLP/0.1 draft-03, Annex D; §8.9",
        "chunk_group_bytes": GROUP,
        "content": {
            "length": length,
            "rule": "byte[i] = (i*7 + 3) mod 256",
            "urn": urn_mlet(root_digest),
            "blake3_hex": root_digest.hex(),
        },
        "group_cvs_hex": [hexcv(cv) for cv in group_cvs],
        "parent_nodes": [
            {
                "pre_order_index": p["pre_order_index"],
                "covers_groups": f"{p['covers'][0]}-{p['covers'][1] - 1}",
                "node_hex": (cv_bytes(p["left_cv"])
                             + cv_bytes(p["right_cv"])).hex(),
                "cv_hex": hexcv(p["cv"]),
            }
            for p in parents
        ],
        "combined": {
            "encoded_length": len(combined),
            "layout": "8-byte LE content length, then pre-order: "
                      "parent nodes (64 B) and group bytes",
            "blake3_hex": blake3.blake3(combined).hexdigest(),
        },
        "slice": {
            "offset": rng[0],
            "length": rng[1],
            "groups": [1, 2],
            "encoded_length": len(sl),
            "layout": "len(8) | root node(64) | left node(64) | "
                      "group-1 bytes(16384) | right node(64) | "
                      "group-2 bytes(16384)",
            "blake3_hex": blake3.blake3(sl).hexdigest(),
        },
        "corrupted_slice": {
            "base": "slice",
            "xor": {"offset": corrupt_at, "mask": 1},
            "expect": {
                "code": "bao-verify-failed",
                "failing_group": 2,
                "note": "every byte before the failing group verified "
                        "and may have been released; nothing of group "
                        "2 may be (Annex D.3)",
            },
        },
    }
    with open("../vectors/mlp-tv-008.json", "w") as f:
        f.write(json.dumps(vector, indent=2, ensure_ascii=False))
    print("TV-008 written; self-checks passed "
          f"(root == blake3, slice groups {verify_slice(sl, root_cv, length, rng)})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
