#!/usr/bin/env python3
"""Seeds the disposable UI backend (port 41888) with a deterministic library.

Creates 73 notes, nested notebooks (3 levels), tags, a conflict, to-dos,
Markdown/XSS fixtures and a long-title note. Writes ids to qa/run/seed.json.
"""
import json
import os
import time
import urllib.parse
import urllib.request

BASE = "http://127.0.0.1:41888"
TOKEN = "ui-token"
HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.join(HERE, "run", "seed.json")


def call(method, path, body=None):
    sep = "&" if "?" in path else "?"
    url = f"{BASE}{path}{sep}token={urllib.parse.quote(TOKEN)}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method,
                                 headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req) as res:
        raw = res.read()
        return json.loads(raw) if raw else None


def folder(title, parent=""):
    return call("POST", "/folders", {"title": title, "parent_id": parent})["id"]


def tag(title):
    return call("POST", "/tags", {"title": title})["id"]


def note(**kw):
    return call("POST", "/notes", kw)


def attach(tag_id, note_id):
    call("POST", f"/tags/{tag_id}/notes", {"id": note_id})


def main():
    now = int(time.time() * 1000)

    work = folder("Work")
    personal = folder("Personal")
    projects = folder("Projects", work)
    q4 = folder("Q4", projects)
    reading = folder("Reading")

    idea = tag("idea")
    urgent = tag("urgent")
    someday = tag("someday")

    ids = {"work": work, "personal": personal, "projects": projects, "q4": q4, "reading": reading,
           "tags": {"idea": idea, "urgent": urgent, "someday": someday}}

    # 100 journal notes give the list more than one keyset page.
    journals = []
    for i in range(1, 101):
        parent = work if i % 2 else personal
        n = note(title=f"Journal {i:02d}",
                 body=f"Entry {i:02d}\n\nThe lake was {['still', 'loud', 'grey', 'bright', 'quiet'][i % 5]} this morning. "
                      f"Entry number {i} of the disposable QA library.",
                 parent_id=parent, created_time=now - i * 60000, updated_time=now - i * 60000)
        journals.append(n["id"])
    ids["journals"] = journals

    markdown = note(
        title="Markdown field guide",
        parent_id=work,
        created_time=now + 1000, updated_time=now + 1000,
        body="""# Field guide

A **bold** claim, an *italic* aside, and `inline code`.

## Stations

1. capture
2. sync
3. read

- [ ] wire the live engine
- [x] name the stations
- not a task [really]

> Nothing was overwritten.

```go
func main() { fmt.Println("hello") }
```

[Project intent](https://example.com/intent)
""")
    ids["markdown"] = markdown["id"]

    xss = note(
        title="XSS probe",
        parent_id=work,
        created_time=now + 2000, updated_time=now + 2000,
        body="""<script>window.__XSS = 1</script>

<img src=x onerror="window.__XSS = 2">

<iframe src="javascript:window.__XSS = 3"></iframe>

<a href="javascript:window.__XSS = 4">click</a>

<div onclick="window.__XSS = 5">raw html</div>

**safe bold survives**
""")
    ids["xss"] = xss["id"]

    todo = note(title="Todo: ship M4", parent_id=work, is_todo=1,
                todo_due=now + 3 * 86400000, created_time=now + 3000, updated_time=now + 3000,
                body="Close the remaining M4 gate items.\n\n- [ ] typed bindings\n- [ ] real-target loops")
    ids["todo"] = todo["id"]

    done = note(title="Todo: done already", parent_id=personal, is_todo=1, todo_completed=now,
                created_time=now + 3100, updated_time=now + 3100,
                body="This one is finished.")
    ids["todo_done"] = done["id"]

    deep = note(title="Deep note: Q4 planning", parent_id=q4,
                created_time=now + 4000, updated_time=now + 4000,
                body="The third-level notebook is reachable from the sidebar.")
    ids["deep"] = deep["id"]

    conflict = note(title="Conflict: office hours", parent_id=work, is_conflict=1,
                    conflict_original_id=journals[0],
                    created_time=now + 5000, updated_time=now + 5000,
                    body="My local version of the office hours note, kept as a conflict copy.")
    ids["conflict"] = conflict["id"]

    tagged = note(title="Tagged: weekend plan", parent_id=personal,
                  created_time=now + 6000, updated_time=now + 6000,
                  body="Hike, bread, and an hour with the sync line.")
    attach(idea, tagged["id"])
    attach(urgent, tagged["id"])
    ids["tagged"] = tagged["id"]

    tagged2 = note(title="Tagged: urgent only", parent_id=work,
                   created_time=now + 6100, updated_time=now + 6100,
                   body="Only the urgent tag is attached here.")
    attach(urgent, tagged2["id"])
    ids["tagged2"] = tagged2["id"]

    long_title = "A very long note title that keeps going and going so the list row and the editor header have to truncate instead of wrapping the layout into a mess " + "x" * 60
    lng = note(title=long_title, parent_id=reading,
               created_time=now + 7000, updated_time=now + 7000,
               body="First line of a long note that should be capped at one hundred and forty runes by the summary API. " + "y" * 240 + "\n\nSecond paragraph.")
    ids["long"] = lng["id"]

    empty = note(title="Empty body note", parent_id=reading,
                 created_time=now + 7100, updated_time=now + 7100, body="")
    ids["empty"] = empty["id"]

    uni = note(title="日本語 — café 🙂", parent_id=personal,
               created_time=now + 8000, updated_time=now + 8000,
               body="Unicode: αβγ · 中文 · العربية · 🙂\n\nSecond line.")
    ids["unicode"] = uni["id"]

    loose = note(title="No notebook", parent_id="",
                 created_time=now + 9000, updated_time=now + 9000,
                 body="This note has no notebook.")
    ids["loose"] = loose["id"]

    os.makedirs(os.path.dirname(OUT), exist_ok=True)
    with open(OUT, "w", encoding="utf-8") as f:
        json.dump(ids, f, indent=2)

    total = call("GET", "/notes?limit=1&fields=id")  # cheap sanity call
    print(json.dumps({"seeded": True, "notes": 112, "notebooks": 5, "tags": 3,
                      "sample": total.get("items", [])}, ensure_ascii=False))


if __name__ == "__main__":
    main()
