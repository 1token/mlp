#!/usr/bin/env python3
"""Generate conformance/MUST-AUDIT.md: the D-104 coverage map. Each
corpus entry (from audit-musts.py) carries a status and its covering
tests. Statuses:

  COVERED   — a test whose input violates the MUST exists and fails
              without the enforcement (the D-104 bar).
  PARTIAL   — enforcement exists and happy paths exercise it, but no
              dedicated failing-input test; or the requirement is
              covered for a subset of its scope (noted).
  OPEN-CLIENT   — binds the client's presentation layer; behind the
                  S3.10/S3.11 client backlog (D-233).
  OPEN-DEFERRED — binds machinery the reference does not implement
                  yet (noted with its decision).
  TRANSITIVE — enforced by byte-identity with frozen vectors (our
               emitters cannot violate it without breaking TV
               reproduction).
  META      — requirement-language boilerplate, not testable.

Run after audit-musts.py; CI executes both and fails on drift.
"""
import re
import sys

CORPUS = 'conformance/must-corpus.txt'
OUT = 'conformance/MUST-AUDIT.md'

# Annotations keyed by corpus id. Every id in the corpus MUST appear
# here — the generator refuses otherwise, so a spec edit that adds a
# requirement forces an audit decision.
A = {
  'M001': ('META', 'RFC 2119 boilerplate (§2.1).'),
  'M002': ('COVERED', 'sn/must_test.go TestJSONConventionRefusals/float_manifest_size'),
  'M003': ('COVERED', 'same case: a float anywhere in a validated document refuses; core/tv001_test.go TestDialectRejections guards the parser'),
  'M004': ('COVERED', 'sn/must_test.go TestJSONConventionRefusals/epoch_timestamp'),
  'M005': ('COVERED', 'sn/must_test.go TestUnknownMemberTolerance (the positive tolerance case); discovery TestUnknownMembersIgnored'),
  'M006': ('COVERED', 'core/tv001_test.go TestTV001 (canonicalize-then-verify against the vector); sn TestVerifyTV002Verdicts'),
  'M007': ('COVERED', 'sn/must_test.go TestJSONConventionRefusals/null_subject + null_envelope_to_entry'),
  'M008': ('COVERED', 'as M005'),
  'M009': ('OPEN-CLIENT', 'display-name caution UI (D-164); client presentation backlog, S3.10/D-233'),
  'M010': ('COVERED', 'sn/must_test.go TestJSONConventionRefusals/duplicate_manifest_urn'),
  'M011': ('OPEN-CLIENT', 'name-is-not-a-path neutralization happens at client save (D-47); server never treats name as a path (no path use exists — grep-verifiable), client save-dialog gate open'),
  'M012': ('COVERED', 'sn/mep_test.go TestTV007PreviewOfValidation (all four violating shapes)'),
  'M013': ('COVERED', 'render TestGoSanitizerReproducesTV005 + client run-tv005.js: unmanifested references removed (corpus cases)'),
  'M014': ('COVERED', 'as M006'),
  'M015': ('COVERED', 'sn TestValidationFailureMatrix/mixed_domains'),
  'M016': ('COVERED', 'sn/mep_test.go (TV-006 suite): strict until validation; malformed until = malformed envelope'),
  'M017': ('COVERED', 'sn TestValidationFailureMatrix/hop_sig_tamper; forward tests verify the current hop'),
  'M018': ('COVERED', 'sn/forward_test.go: automatic re-dispatch into the chain refused (D-51)'),
  'M019': ('COVERED', 'sn TestValidationFailureMatrix/oversized'),
  'M020': ('COVERED', 'sn TestValidationFailureMatrix/non-local_recipient'),
  'M021': ('COVERED', 'sn materialization asserted across the suite (threads/messages/refs after ingest); cmd/mlpd TestTwoDomainDemo end to end'),
  'M022': ('COVERED', 'sn/must_test.go TestAddressGrammarRefusals (single-label + six more violations)'),
  'M023': ('PARTIAL', 'the server enforces the LDH + hyphen-position subset (TestAddressGrammarRefusals, the audit-found underscore fix); full IDNA2008 U-label conversion is user-input handling in clients (§4.3) — untested there'),
  'M024': ('OPEN-CLIENT', 'A-label rendering rule for non-correspondents (D-164); client backlog'),
  'M025': ('OPEN-CLIENT', 'display-name-as-address refusal; client backlog'),
  'M026': ('OPEN-CLIENT', 'caution UI; client backlog'),
  'M027': ('OPEN-DEFERRED', 'the DNS hint path (§5.3) is not implemented in the reference — HTTPS-only discovery; the MUST binds implementations that process hints (D-12). Deferred with §5.3.'),
  'M028': ('COVERED', 'discovery TestDomainBindingHardFails / TestVersionIntersectionHardFails / TestMissingRequiredMemberHardFails'),
  'M029': ('OPEN-DEFERRED', 'hint/HTTPS disagreement hard-fail: as M027, deferred with the hint machinery'),
  'M030': ('COVERED', 'discovery TestDomainBindingHardFails (the D-57 binding check)'),
  'M031': ('COVERED', 'discovery TestVerificationKeySemantics (windows); bs TestFreshnessWindowAndRoles for transfer keys'),
  'M032': ('OPEN-DEFERRED', '§5.3 hint grammar (v=MLP1): as M027'),
  'M033': ('OPEN-DEFERRED', '§5.3 hint url https: as M027'),
  'M034': ('COVERED', 'discovery TestFetchSizeCapAborts'),
  'M035': ('COVERED', 'discovery TestDialTimeAddressCheckWired (resolve-then-pin at connect time)'),
  'M036': ('COVERED', 'discovery TestResolverCacheCeilingAndKidRefetch (unknown-kid refetch)'),
  'M037': ('PARTIAL', 'per-user resolution (§5.6) is not implemented (optional); the rate-limit MUST binds enablers — recorded here so enabling it forces the test'),
  'M038': ('COVERED', 'discovery TestAlgMismatchIgnoresEntry (multicodec/alg agreement)'),
  'M039': ('COVERED', 'discovery TestKidSelfVerificationIgnoresEntry'),
  'M040': ('COVERED', 'discovery TestKidSelfVerificationIgnoresEntry + TestDuplicateKidIgnored'),
  'M041': ('META', 'MUST NOT implement primitives yourself — a build/dependency policy (stdlib crypto only; go.mod is the witness), not input-testable'),
  'M042': ('COVERED', 'bs TestFreshnessWindowAndRoles (alg enforcement + windows on transfer signatures)'),
  'M043': ('COVERED', 'sn TestRetryIdempotencyAndReplay (recorded dispatch answered with the current snapshot)'),
  'M044': ('COVERED', 'sn TestUnknownRecipientAndCompleteness (each envelope_to entry exactly once)'),
  'M045': ('COVERED', 'sn TestUnknownRecipientAndCompleteness (every Manifest urn exactly once in media)'),
  'M046': ('COVERED', 'bs TestHardenedPusherRefusesLoopbackAndHTTP (D-72 on the pushing side)'),
  'M047': ('COVERED', 'sn verdict parsing refuses non-https reservation target_url (§7.5); exercised by the S4.13 AllowInsecureTransport branch in reverse — the production default path is the refusal, cmd/mlpd tests run the demo branch and sn/verdict tests the default'),
  'M048': ('COVERED', 'sn TestOriginTransitionWalk + store TestReplayUniqueAndReservationTerminal (terminal verdicts immutable)'),
  'M049': ('META', 'the §7.7 mechanics/policy split statement; the mechanics MUSTs are audited individually above'),
  'M050': ('COVERED', 'as M046 (the §8.2 restatement of D-72)'),
  'M051': ('COVERED', 'bs TestOffsetMismatchAndHashReset + TestDigestMismatchRollsBack (the PATCH N rules)'),
  'M052': ('OPEN-DEFERRED', 'segments (§8.6) are unimplemented in the reference BS (single-digest verification only); the length-equality MUST lands with segment support. Recorded as the largest deferred wire feature.'),
  'M053': ('COVERED', 'sn/forward_test.go TestForwardReproducesTV004Envelope (complete chain carried)'),
  'M054': ('COVERED', 'forward tests: the root origin guaranteed present in sources'),
  'M055': ('COVERED', 'sn/must_test.go TestNonChainSourceNeverContacted (receiver side)'),
  'M056': ('COVERED', 'same test: the interloper is ignored, the chain member still asked'),
  'M057': ('COVERED', 'sn/gc_test.go TestGCInvariants (the tombstone minimum record survives the flip)'),
  'M058': ('OPEN-CLIENT', 'tombstone rendering; client backlog'),
  'M059': ('COVERED', 'sn/gc_test.go TestGCInvariants (pinned retains absolutely — §10.5 invariant 1)'),
  'M060': ('COVERED', 'clientapi media tests: recipients-only raw endpoint + hardened object serving (S4.11); §10.7 access answered for owners'),
  'M061': ('COVERED', 'render/client TV-005 corpus: media element attribute stripping'),
  'M062': ('OPEN-CLIENT', 'no-autoplay presentation; the sanitizer strips autoplay (TV-005) — the play-with-controls duty is presentation'),
  'M063': ('COVERED', 'client TV-005 + viewer: external links noopener/noreferrer with title disclosure (run-html.js discipline + viewer tests)'),
  'M064': ('COVERED', 'client run-tv005.js: render-time re-sanitization is the pipeline under test (D-31 dual duty)'),
  'M065': ('COVERED', 'TV-005 idempotence: both implementations, all 14 cases (sanitize∘sanitize = sanitize)'),
  'M066': ('COVERED', 'client viewer: shadow-DOM isolation (mlp-body-viewer tests; the §11.7 floor)'),
  'M067': ('COVERED', 'bs transactional verification fronts the store (TestTranscriptWalk, TestRestartRederivation); §14.3 restates §8'),
  'M068': ('META', 'the D-104 principle itself'),
  'M069': ('META', 'the D-104 principle, continued'),
}

def main():
    rows = []
    for line in open(CORPUS):
        m = re.match(r'^(M\d{3}) \[([^\]]*)\] \((\d+)\) (.*)$', line)
        if m:
            rows.append(m.groups())
    missing = [r[0] for r in rows if r[0] not in A]
    extra = [k for k in A if k not in {r[0] for r in rows}]
    if missing or extra:
        print(f"audit drift: unannotated {missing}, stale {extra}", file=sys.stderr)
        return 1
    counts = {}
    for r in rows:
        st = A[r[0]][0]
        counts[st] = counts.get(st, 0) + 1
    testable = len(rows) - counts.get('META', 0)
    covered = counts.get('COVERED', 0) + counts.get('TRANSITIVE', 0)
    with open(OUT, 'w') as f:
        f.write("# The D-104 MUST audit — MLP/0.1 draft-02\n\n")
        f.write("Generated by `audit-musts.py` + `audit-annotate.py`; CI regenerates\n")
        f.write("both and fails on drift, so a spec edit that adds a requirement\n")
        f.write("forces an audit decision before merge.\n\n")
        f.write(f"**{len(rows)} corpus entries.** ")
        f.write(f"Of the {testable} testable requirements: ")
        f.write(f"**{covered} COVERED** by failing-input or vector tests, ")
        f.write(f"{counts.get('PARTIAL',0)} PARTIAL, ")
        f.write(f"{counts.get('OPEN-CLIENT',0)} OPEN-CLIENT (presentation-layer, "
                "S3.10/S3.11 backlog per D-233), ")
        f.write(f"{counts.get('OPEN-DEFERRED',0)} OPEN-DEFERRED (unimplemented "
                "optional machinery, each tied to its decision).\n\n")
        f.write("| # | Section | Requirement | Status | Coverage |\n")
        f.write("|---|---------|-------------|--------|----------|\n")
        for mid, sec, n, text in rows:
            st, note = A[mid]
            short = text if len(text) <= 110 else text[:107] + '…'
            short = short.replace('|', '\\|')
            note = note.replace('|', '\\|')
            f.write(f"| {mid} | {sec} | {short} | **{st}** | {note} |\n")
    print(f"MUST-AUDIT.md: {len(rows)} entries — {covered}/{testable} covered, "
          f"{counts.get('PARTIAL',0)} partial, "
          f"{counts.get('OPEN-CLIENT',0)}+{counts.get('OPEN-DEFERRED',0)} open, "
          f"{counts.get('META',0)} meta")
    return 0

if __name__ == '__main__':
    sys.exit(main())
