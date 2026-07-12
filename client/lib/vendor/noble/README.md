# Vendored: @noble/hashes 1.4.0 (blake3 closure)

Six ESM files from the audited `@noble/hashes` package (MIT), plus
`crypto.js`, vendored verbatim except for two mechanical patches: the bare
specifier `@noble/hashes/crypto` in `utils.js` becomes `./crypto.js`
so the files load as plain browser ES modules with no build step
(D-116), and every file gains a `// @ts-nocheck` header — vendored
code is exempt from the project's JSDoc discipline; the typed
surface is `lib/mlet-urn.js`. Pure JS — no wasm, so the client CSP keeps
`script-src 'self'` with no `wasm-unsafe-eval` concession (D-244).

Upstream: https://github.com/paulmillr/noble-hashes @ 1.4.0
