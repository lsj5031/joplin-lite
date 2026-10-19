#!/usr/bin/env python3
"""Single-writer helper for the canonical QA spreadsheet (qa/features.csv).

The CSV is the source of truth. This script only rewrites individual cells and
appends an event to qa/activity.jsonl so the live dashboard can show progress.

Usage:
  python3 qa/track.py set QA-012 pass --evidence "API-4" --note "..."
  python3 qa/track.py set QA-012 fail --severity high --error "what broke"
  python3 qa/track.py fix QA-012 --fix "what changed"
  python3 qa/track.py edit QA-012 --expected "the rewritten expectation"
  python3 qa/track.py note QA-012 "..."
  python3 qa/track.py log "phase 2 started"
  python3 qa/track.py summary
"""
import csv
import json
import os
import sys
from datetime import datetime, timezone

QA = os.path.dirname(os.path.abspath(__file__))
CSV = os.path.join(QA, "features.csv")
ACTIVITY = os.path.join(QA, "activity.jsonl")
FIELDS = [
    "id", "area", "feature", "user_story", "expected", "status", "severity",
    "layer", "evidence", "error", "fix", "notes", "updated",
]


def now():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def load():
    with open(CSV, newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def save(rows):
    tmp = CSV + ".tmp"
    with open(tmp, "w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=FIELDS)
        w.writeheader()
        w.writerows(rows)
    os.replace(tmp, CSV)


def activity(kind, row, message):
    with open(ACTIVITY, "a", encoding="utf-8") as f:
        f.write(json.dumps({
            "at": now(), "kind": kind, "id": row.get("id", ""),
            "area": row.get("area", ""), "feature": row.get("feature", ""),
            "status": row.get("status", ""), "message": message,
        }, ensure_ascii=False) + "\n")


def find(rows, ident):
    for r in rows:
        if r["id"] == ident:
            return r
    raise SystemExit(f"no such id: {ident}")


def opt(args, name, default=""):
    if name in args:
        i = args.index(name)
        if i + 1 >= len(args):
            raise SystemExit(f"{name} needs a value")
        return args[i + 1]
    return default


def main():
    if len(sys.argv) < 2:
        raise SystemExit(__doc__)
    cmd, args = sys.argv[1], sys.argv[2:]

    if cmd == "log":
        message = " ".join(args) or "update"
        row = {"id": "", "area": "", "feature": "", "status": ""}
        activity("phase", row, message)
        print(f"logged: {message}")
        return

    rows = load()

    if cmd == "summary":
        counts = {}
        for r in rows:
            counts[r["status"]] = counts.get(r["status"], 0) + 1
        print(json.dumps({"total": len(rows), **counts}, indent=2))
        return

    if cmd == "import":
        if not args:
            raise SystemExit("import needs a results JSON path")
        payload = json.load(open(args[0], encoding="utf-8"))
        results = payload.get("results", payload)
        touched = 0
        for ident, res in results.items():
            row = find(rows, ident)
            note = opt(args, "--note", "")
            for col in ("status", "severity", "evidence", "error", "fix", "notes"):
                if col in res and res[col] != "" and res[col] is not None:
                    row[col] = str(res[col])
            if row["status"] == "pass":
                # A story that now passes must not keep a stale blocked/fail reason.
                row["error"] = ""
                row["severity"] = "-"
            if note:
                row["notes"] = (row["notes"] + " " + note).strip()
            row["updated"] = now()
            activity("status", row, f"→ {row['status']}"
                     + (f" — {row['error'][:160]}" if row["error"] and row["status"] in ("fail", "blocked", "gap") else ""))
            touched += 1
        save(rows)
        print(f"imported {touched} rows from {args[0]}")
        return

    if not args:
        raise SystemExit(__doc__)
    ident = args[0]
    row = find(rows, ident)

    if cmd == "note":
        row["notes"] = " ".join(args[1:])
        row["updated"] = now()
        activity("note", row, row["notes"])
    elif cmd == "fix":
        row["fix"] = opt(args, "--fix")
        row["status"] = "fixed"
        row["updated"] = now()
        activity("fixed", row, f"fix: {row['fix'][:160]}")
    elif cmd == "edit":
        for name, col in (("--expected", "expected"), ("--user-story", "user_story"),
                          ("--feature", "feature")):
            value = opt(args, name)
            if value:
                row[col] = value
        row["updated"] = now()
        activity("edit", row, "rewrote " + ", ".join(
            c for n, c in (("--expected", "expected"), ("--user-story", "user_story"),
                           ("--feature", "feature")) if opt(args, n)))
    elif cmd == "set":
        if len(args) < 2:
            raise SystemExit("set needs <id> <status>")
        row["status"] = args[1]
        for name, col in (("--severity", "severity"), ("--evidence", "evidence"),
                          ("--error", "error"), ("--fix", "fix"), ("--note", "notes")):
            value = opt(args, name)
            if value:
                row[col] = value
        row["updated"] = now()
        activity("status", row, f"→ {row['status']}" + (f" — {row['error'][:160]}" if row["error"] and row["status"] in ("fail", "blocked", "gap") else ""))
    else:
        raise SystemExit(__doc__)

    save(rows)
    print(f"{row['id']} [{row['status']}] {row['feature']}")


if __name__ == "__main__":
    main()
