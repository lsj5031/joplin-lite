#!/usr/bin/env python3
"""Seed one QA backend.

The api/ro/allow-list backends need pre-existing data their real policies can
only read, so qa/reset.sh runs this against a temporary wildcard policy pointed
at the same data dir.

    python3 qa/seed_backend.py <port> <token> <api|ro|allow>
"""
import json
import sys
import urllib.parse
import urllib.request

port, token, kind = sys.argv[1], sys.argv[2], sys.argv[3]
BASE = f"http://127.0.0.1:{port}"


def call(path, method="GET", body=None):
    url = f"{BASE}{path}{'&' if '?' in path else '?'}token={token}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req) as r:
        return json.loads(r.read() or b"null")


def ensure_folder(title, parent=""):
    for f in call("/folders?limit=100")["items"]:
        if f["title"] == title:
            return f["id"]
    return call("/folders", "POST", {"title": title, "parent_id": parent})["id"]


def ensure_tag(title):
    for t in call("/tags?limit=100")["items"]:
        if t["title"] == title:
            return t["id"]
    return call("/tags", "POST", {"title": title})["id"]


def ensure_note(title, body, parent="", tag_ids=None):
    for n in call(f"/notes?summary=1&query={urllib.parse.quote(title)}&limit=100"
                  + "&include_conflicts=1")["items"]:
        if n["title"] == title:
            return n["id"]
    payload = {"title": title, "body": body, "parent_id": parent}
    if tag_ids:
        payload["tag_ids"] = tag_ids
    return call("/notes", "POST", payload)["id"]


def main():
    if kind == "api":
        inbox = ensure_folder("Inbox")
        work = ensure_tag("work")
        ensure_note("Alpha plan", "Kickoff notes for the alpha milestone.", inbox, [work])
        ensure_note("Beta review", "Review checklist and open questions.")
        ensure_note("Gamma draft", "Draft body with the word lavender inside.")
    elif kind == "ro":
        folder = ensure_folder("RO folder")
        ensure_note("RO seed note", "This note exists so read-only updates can be refused.", folder)
    elif kind == "allow":
        allowed = ensure_folder("Allowed")
        ensure_folder("Other")
        green = ensure_tag("green")
        ensure_tag("red")
        ensure_note("Groceries", "Milk, bread, coffee.", allowed, [green])
    print(json.dumps({"kind": kind, "ok": True}))


if __name__ == "__main__":
    main()
