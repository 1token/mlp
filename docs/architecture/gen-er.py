#!/usr/bin/env python3
"""Generate docs/architecture/DATA-MODEL.md from the real migrations
(server/store/migrations/*.sql): PlantUML entity diagrams per
subsystem with in-group FK edges and cross-group references as
prose. Regenerate after any migration; the doc is generated output,
edited only through this script."""
import re
import glob

MIGRATIONS = 'server/store/migrations/*.sql'
OUT = 'docs/architecture/DATA-MODEL.md'

GROUPS = {
    'Federation core (dispatch, verdicts, delegation, discovery)':
        ['medialets', 'envelopes_in', 'dispatches', 'dispatch_recipients',
         'verdicts', 'verdict_media', 'delegations',
         'domain_docs', 'domain_keys', 'own_keys'],
    'Mailbox & threading':
        ['mailboxes', 'threads', 'messages', 'refs', 'correspondents',
         'labels', 'thread_labels', 'ref_labels', 'rules', 'drafts',
         'undo_journal', 'settings', 'events', 'idempotency'],
    'Transfer & storage':
        ['stores', 'objects', 'reservations_in', 'reservations_out'],
    'Deliveries & guests':
        ['deliveries', 'timeline_events', 'guest_links', 'guest_downloads'],
    'Identity & sessions':
        ['credentials', 'password_fallback', 'recovery_codes',
         'recovery_email', 'sessions',
         'webauthn_credentials', 'webauthn_challenges'],
}


def parse():
    sql = ''
    for f in sorted(glob.glob(MIGRATIONS)):
        sql += open(f).read() + '\n'
    tables = {}
    # Multi-line and single-line CREATE TABLE bodies alike: match the
    # balanced-enough form ending in ');' at line end.
    for m in re.finditer(r'CREATE TABLE (\w+)\s*\((.*?)\);\s*(?:--[^\n]*)?$',
                         sql, re.S | re.M):
        name, body = m.group(1), m.group(2)
        cols = []
        # Split on commas that are not inside parentheses.
        parts, depth, cur = [], 0, ''
        for ch in body:
            if ch == '(':
                depth += 1
            elif ch == ')':
                depth -= 1
            if ch == ',' and depth == 0:
                parts.append(cur)
                cur = ''
            else:
                cur += ch
        parts.append(cur)
        for part in parts:
            line = ' '.join(x.split('--')[0].strip() for x in part.split('\n'))
            line = line.strip().rstrip(',').strip()
            if not line or re.match(r'(PRIMARY KEY|UNIQUE|CHECK|FOREIGN KEY)\b', line):
                continue
            cm = re.match(r'(\w+)\s+(\w+)', line)
            if cm:
                fk = re.search(r'REFERENCES (\w+)', line)
                cols.append((cm.group(1), cm.group(2),
                             'PRIMARY KEY' in line, fk.group(1) if fk else None))
        tables[name] = cols
    for m in re.finditer(r'ALTER TABLE (\w+) ADD COLUMN (\w+)\s+(\w+)', sql):
        tables[m.group(1)].append((m.group(2), m.group(3), False, None))
    return tables


def main():
    tables = parse()
    grouped = sum(GROUPS.values(), [])
    missing = [t for t in tables if t not in grouped]
    stale = [t for t in grouped if t not in tables]
    if missing or stale:
        raise SystemExit(f'gen-er drift: ungrouped {missing}, stale {stale}')
    with open(OUT, 'w') as out:
        out.write("""# Data model — the reference schema, by subsystem

Generated from `server/store/migrations/` (0001–0005) by
`docs/architecture/gen-er.py`; regenerate after any migration. The
§10.3 reference state machine on `refs` is enforced by a database
trigger (see 0001), so illegal transitions fail at the storage
layer, not by convention. Sensitive columns (`seed`, `token_hash`,
`pin_hash`, `session_hash`) hold seeds or hashes exactly as the
decisions demand — a database leak must not mint working
capabilities (D-155).

""")
        for title, names in GROUPS.items():
            slug = re.sub(r'[^a-z]+', '-', title.lower()).strip('-')
            out.write(f"## {title}\n\n```plantuml\n@startuml er-{slug}\n"
                      "!theme plain\nhide circle\nskinparam linetype ortho\n\n")
            for t in names:
                out.write(f"entity {t} {{\n")
                for col, typ, pk, fk in tables[t]:
                    out.write(f"  {'*' if pk else ''}{col} : {typ}\n")
                out.write("}\n")
            for t in names:
                for col, typ, pk, fk in tables[t]:
                    if fk and fk in names:
                        out.write(f"{t} }}o--|| {fk} : {col}\n")
            out.write("@enduml\n```\n\n")
            crossers = [f"`{t}.{col}` → `{fk}`"
                        for t in names
                        for col, typ, pk, fk in tables[t]
                        if fk and fk not in names]
            if crossers:
                out.write("Cross-subsystem references: "
                          + "; ".join(crossers) + ".\n\n")
    print(f"DATA-MODEL.md: {len(tables)} tables in {len(GROUPS)} subsystems")


if __name__ == '__main__':
    main()
