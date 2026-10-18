#!/usr/bin/env python3
"""User-story API suite for joplin-lite.

Exercises the Clipper REST API against the disposable QA backends and writes
qa/results-api.json keyed by story id.

    python3 qa/test_api.py            # everything
    python3 qa/test_api.py API-22 API-43
"""
import json
import re
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
RESULTS = Path(__file__).resolve().parent / "results-api.json"

API, TOKEN = "http://127.0.0.1:41890", "api-token"
RO, RO_TOKEN = "http://127.0.0.1:41891", "ro-token"
ALLOW, ALLOW_TOKEN = "http://127.0.0.1:41892", "allow-token"
ZERO = "0" * 32
HEX32 = re.compile(r"^[0-9a-f]{32}$")

only = set(sys.argv[1:])
results = {}
created_notes = []
created_folders = []
created_tags = []
created_resources = []


def request(base, path, method="GET", body=None, token=TOKEN, headers=None,
            data=None, ctype="application/json"):
    sep = "&" if "?" in path else "?"
    url = f"{base}{path}" + (f"{sep}token={urllib.parse.quote(token)}" if token else "")
    payload = data
    if payload is None and body is not None:
        payload = json.dumps(body).encode()
        headers = {"Content-Type": ctype, **(headers or {})}
    req = urllib.request.Request(url, data=payload, method=method, headers=headers or {})
    try:
        with urllib.request.urlopen(req) as r:
            raw = r.read()
            hdrs = {k.lower(): v for k, v in r.headers.items()}
            return r.status, raw, hdrs
    except urllib.error.HTTPError as e:
        return e.code, e.read(), {k.lower(): v for k, v in e.headers.items()}


def jrequest(base, path, method="GET", **kw):
    status, raw, hdrs = request(base, path, method, **kw)
    try:
        data = json.loads(raw or b"null")
    except ValueError:
        data = raw.decode("utf-8", "replace")
    return status, data, hdrs


def ok(base, path, method="GET", **kw):
    status, data, _ = jrequest(base, path, method, **kw)
    assert status == 200, f"{method} {path} -> {status}: {data}"
    return data


def note(title, body="body", tag_ids=None, **extra):
    payload = {"title": title, "body": body, **extra}
    if tag_ids:
        payload["tag_ids"] = tag_ids
    n = ok(API, "/notes", "POST", body=payload)
    created_notes.append(n["id"])
    return n


def folder(title, parent=""):
    f = ok(API, "/folders", "POST", body={"title": title, "parent_id": parent})
    created_folders.append(f["id"])
    return f


def tag(title):
    t = ok(API, "/tags", "POST", body={"title": title})
    created_tags.append(t["id"])
    return t


def eq(actual, expected, msg):
    assert actual == expected, f"{msg}: {actual!r} != {expected!r}"


def truthy(value, msg):
    assert value, msg


def cleanup():
    for rid in created_resources:
        request(API, f"/resources/{rid}", "DELETE")
    for nid in created_notes:
        request(API, f"/notes/{nid}", "DELETE")
    for fid in created_folders:
        request(API, f"/folders/{fid}", "DELETE")
    for tid in created_tags:
        request(API, f"/tags/{tid}", "DELETE")
    created_notes.clear()
    created_folders.clear()
    created_tags.clear()
    created_resources.clear()


def main():
    tests = []

    def test(story_id, name):
        def deco(fn):
            tests.append((story_id, name, fn))
            return fn
        return deco

    # ---------------------------------------------------------------- open/health
    @test("API-01", "ping is open")
    def _():
        status, raw, hdrs = request(API, "/ping", token=None)
        eq(status, 200, "ping status")
        eq(raw.decode(), "JoplinClipperServer", "ping body")
        truthy("text/plain" in hdrs.get("content-type", ""), "ping content type")

    @test("API-02", "health is open and empty")
    def _():
        status, raw, _ = request(API, "/health", token=None)
        eq(status, 200, "health status")
        eq(raw, b"", "health body")

    @test("API-03", "token is required")
    def _():
        status, body, _ = jrequest(API, "/notes", token=None)
        eq(status, 403, "missing token status")
        truthy(isinstance(body, dict) and "error" in body, "json error body")

    @test("API-04", "invalid token is rejected")
    def _():
        status, body, _ = jrequest(API, "/notes", token="wrong")
        eq(status, 403, "wrong token status")
        eq(body.get("error"), 'Invalid "token" parameter', "error text")

    @test("API-05", "bearer key authenticates")
    def _():
        status, body, _ = jrequest(API, "/notes?limit=1", token=None,
                                   headers={"Authorization": "Bearer api-key"})
        eq(status, 200, "bearer status")
        truthy("items" in body, "bearer response")
        status2, _, _ = jrequest(API, "/notes?limit=1", token=None,
                                 headers={"Authorization": "Bearer api-token"})
        eq(status2, 200, "bearer token value")

    # ---------------------------------------------------------------- pairing
    @test("API-06", "pairing request starts")
    def _():
        status, body, _ = jrequest(API, "/auth", "POST", body={}, token=None)
        eq(status, 200, "auth status")
        truthy(HEX32.match(body.get("auth_token", "")), "32-hex auth token")

    @test("API-07", "pairing polls as waiting")
    def _():
        # A wired prompt settles instantly in the headless rig, so the pending
        # state is asserted by the unit test; here we check the HTTP contract.
        token = ok(API, "/auth", "POST", body={}, token=None)["auth_token"]
        _, body, _ = jrequest(API, "/auth/check?auth_token=" + token, token=None)
        truthy(body.get("status") in ("waiting", "accepted", "rejected"),
               f"status for a known token: {body}")
        status, _, _ = jrequest(API, "/auth/check?auth_token=" + ZERO, token=None)
        eq(status, 400, "unknown auth_token rejected")
        run_go_test("TestClipperAuthHandshake")

    @test("API-08", "pairing accept hands over the token")
    def _():
        run_go_test("TestClipperAuthHandshake")

    @test("API-09", "pairing reject never yields the token")
    def _():
        run_go_test("TestClipperAuthHandshake")

    @test("API-10", "web origins cannot pair")
    def _():
        status, _, _ = jrequest(API, "/auth", "POST", body={}, token=None,
                                headers={"Origin": "https://evil.example"})
        eq(status, 403, "web-origin POST /auth")
        status, _, _ = jrequest(API, "/auth/check?token=api-token", token=None,
                                headers={"Origin": "https://evil.example"})
        eq(status, 403, "web-origin GET /auth/check")

    @test("API-11", "token validation endpoint")
    def _():
        _, body, _ = jrequest(API, "/auth/check?token=api-token", token=None)
        eq(body, {"valid": True}, "valid token")
        _, body, _ = jrequest(API, "/auth/check?token=nope", token=None)
        eq(body, {"valid": False}, "invalid token")
        status, body, _ = jrequest(API, "/auth/check", token=None)
        eq(status, 400, "no params")
        eq(body.get("error"), "Invalid request", "no params error")

    # ---------------------------------------------------------------- notes read
    @test("API-12", "notes list newest first")
    def _():
        for i in range(12):
            note(f"API-12 note {i}")
        body = ok(API, "/notes?limit=10")
        eq(len(body["items"]), 10, "page size")
        times = [n["updated_time"] for n in body["items"]]
        eq(times, sorted(times, reverse=True), "updated_time desc")
        truthy(body["has_more"] is True, "has_more")
        truthy("body" in body["items"][0], "full notes carry body")

    @test("API-13", "pagination is capped and offset")
    def _():
        body = ok(API, "/notes?limit=1000")
        truthy(len(body["items"]) <= 100, "limit capped at 100")
        p1 = ok(API, "/notes?limit=2&page=1")
        p2 = ok(API, "/notes?limit=2&page=2")
        eq(len(p1["items"]), 2, "first page")
        ids1 = {n["id"] for n in p1["items"]}
        ids2 = {n["id"] for n in p2["items"]}
        truthy(ids1.isdisjoint(ids2), "pages do not overlap")

    @test("API-14", "fields projection")
    def _():
        first = ok(API, "/notes?limit=1")["items"][0]
        body = ok(API, "/notes?limit=1&fields=id,title")
        eq(set(body["items"][0].keys()), {"id", "title"}, "list fields")
        single = ok(API, f"/notes/{first['id']}?fields=id,title")
        eq(set(single.keys()), {"id", "title"}, "single fields")

    @test("API-15", "ordering")
    def _():
        body = ok(API, "/notes?limit=100&order_by=title&order_dir=asc")
        titles = [n["title"] for n in body["items"]]
        eq(titles, sorted(titles), "title asc")
        # Unknown columns fall back to updated_time DESC.
        fallback = ok(API, "/notes?limit=10&order_by=nonsense")
        times = [n["updated_time"] for n in fallback["items"]]
        eq(times, sorted(times, reverse=True), "fallback updated_time desc")

    @test("API-16", "summary rows")
    def _():
        t = tag("API-16 tag")
        n = note("API-16 long note", body="x" * 300, tag_ids=[t["id"]])
        body = ok(API, f"/notes?summary=1&query=API-16&limit=50&include_conflicts=1")
        row = next(i for i in body["items"] if i["id"] == n["id"])
        truthy("body" not in row, "no body in summary")
        truthy(len(row["excerpt"]) <= 140, f"excerpt length {len(row['excerpt'])}")
        eq(row["tag_ids"], [t["id"]], "tag_ids present")

    @test("API-17", "summary keyset cursor")
    def _():
        for i in range(5):
            note(f"API-17 note {i}")
        seen = []
        cursor = ""
        for _page in range(10):
            path = "/notes?summary=1&limit=2&include_conflicts=1&query=API-17"
            if cursor:
                path += "&cursor=" + urllib.parse.quote(cursor)
            body = ok(API, path)
            seen.extend(i["id"] for i in body["items"])
            if not body.get("has_more"):
                break
            cursor = body["next_cursor"]
            truthy(cursor, "next_cursor present while has_more")
        eq(len(seen), len(set(seen)), "no duplicate rows")
        truthy(len(seen) >= 5, f"paged through all rows ({len(seen)})")

    @test("API-18", "conflict listing flag")
    def _():
        t = tag("API-18 tag")
        n = note("API-18 conflict", body="local", is_conflict=1, conflict_original_id=ZERO,
                 tag_ids=[t["id"]])
        default = ok(API, "/notes?summary=1&query=API-18%20conflict&limit=10")
        eq([i["id"] for i in default["items"]], [], "excluded by default")
        included = ok(API, "/notes?summary=1&query=API-18%20conflict&limit=10&include_conflicts=1")
        eq([i["id"] for i in included["items"]], [n["id"]], "included on request")
        # The flag is a list-wide contract: tag-scoped lists and search must
        # agree with /notes in both full and summary mode.
        for base in (f"/tags/{t['id']}/notes?limit=10",
                     "/search?query=API-18%20conflict&limit=10"):
            for mode in ("", "&summary=1"):
                path = base + mode
                got = ok(API, path)
                eq([i["id"] for i in got["items"]], [], f"excluded by default: {path}")
                got = ok(API, path + "&include_conflicts=1")
                eq([i["id"] for i in got["items"]], [n["id"]], f"included on request: {path}")

    @test("API-19", "note filters")
    def _():
        f = folder("API-19 folder")
        t = tag("API-19 tag")
        n = note("API-19 scoped note", body="scoped lavender token", tag_ids=[t["id"]],
                 parent_id=f["id"])
        by_folder = ok(API, f"/notes?summary=1&parent_id={f['id']}&limit=10")
        truthy(any(i["id"] == n["id"] for i in by_folder["items"]), "parent_id filter")
        by_tag = ok(API, f"/notes?summary=1&tag_id={t['id']}&limit=10")
        truthy(any(i["id"] == n["id"] for i in by_tag["items"]), "tag_id filter")
        by_query = ok(API, "/notes?summary=1&query=scoped%20lavender&limit=10")
        truthy(any(i["id"] == n["id"] for i in by_query["items"]), "query filter (FTS)")

    # ---------------------------------------------------------------- notes write
    @test("API-20", "create note minimum fields")
    def _():
        n = note("API-20 fresh note", body="hello")
        truthy(HEX32.match(n["id"]), "generated id")
        truthy(n["created_time"] > 0 and n["updated_time"] > 0, "timestamps")
        truthy(n["user_created_time"] > 0, "user timestamp")
        eq(n["markup_language"], 1, "markdown markup")
        eq(n.get("type_"), 1, "type_ is note")

    @test("API-21", "create with tags rejects unknowns")
    def _():
        t = tag("API-21 tag")
        n = note("API-21 tagged", tag_ids=[t["id"]])
        ids = [x["id"] for x in ok(API, f"/notes/{n['id']}/tags?limit=10")["items"]]
        eq(ids, [t["id"]], "tag attached at create")
        before = ok(API, "/notes?summary=1&query=API-21%20unknown&limit=10&include_conflicts=1")
        status, body, _ = jrequest(API, "/notes", "POST",
                                   body={"title": "API-21 unknown", "tag_ids": [ZERO]})
        eq(status, 400, "unknown tag rejected")
        after = ok(API, "/notes?summary=1&query=API-21%20unknown&limit=10&include_conflicts=1")
        eq(len(after["items"]), len(before["items"]), "no note created")

    @test("API-22", "create accepts body_html")
    def _():
        n = note("API-22 html", body_html="<p>Hi <b>there</b></p>")
        truthy("<b>" not in n.get("body", ""), "html not stored raw")
        truthy("**there**" in n.get("body", ""), f"converted to markdown: {n.get('body')!r}")

    @test("API-23", "get missing note is 404")
    def _():
        status, body, _ = jrequest(API, f"/notes/{ZERO}")
        eq(status, 404, "status")
        eq(body.get("error"), "not found", "error text")

    @test("API-24", "update note fields")
    def _():
        n = note("API-24 before")
        f = folder("API-24 folder")
        updated = ok(API, f"/notes/{n['id']}", "PUT", body={
            "title": "API-24 after", "body": "new body", "parent_id": f["id"],
            "is_todo": 1, "todo_completed": 123, "todo_due": 456,
            "source_url": "https://example.com/x", "author": "QA", "order": 0.5,
        })
        eq(updated["title"], "API-24 after", "title")
        eq(updated["body"], "new body", "body")
        eq(updated["parent_id"], f["id"], "parent")
        eq(updated["is_todo"], 1, "is_todo")
        eq(updated["todo_completed"], 123, "todo_completed")
        eq(updated["todo_due"], 456, "todo_due")
        eq(updated["source_url"], "https://example.com/x", "source_url")
        eq(updated["author"], "QA", "author")
        fetched = ok(API, f"/notes/{n['id']}")
        eq(fetched["title"], "API-24 after", "persisted")

    @test("API-25", "update accepts body_html")
    def _():
        n = note("API-25 note", body="old")
        updated = ok(API, f"/notes/{n['id']}", "PUT", body={"body_html": "<p>New <i>text</i></p>"})
        truthy("*text*" in updated["body"], f"converted: {updated['body']!r}")
        same = ok(API, f"/notes/{n['id']}", "PUT", body={"body_html": ""})
        eq(same["body"], updated["body"], "empty html leaves body alone")

    @test("API-26", "update missing note is 404")
    def _():
        status, body, _ = jrequest(API, f"/notes/{ZERO}", "PUT", body={"title": "x"})
        eq(status, 404, "status")
        eq(body.get("error"), "not found", "error text")

    @test("API-27", "delete note queues sync")
    def _():
        before = max_event_id()
        n = note("API-27 doomed")
        ok(API, f"/notes/{n['id']}", "DELETE")
        eq(jrequest(API, f"/notes/{n['id']}")[0], 404, "note gone")
        events = events_since(before)
        truthy(any(e.get("item_id") == n["id"] and e.get("type") == 3 for e in events),
               "delete event queued")

    # ---------------------------------------------------------------- folders
    @test("API-28", "folders CRUD")
    def _():
        root = folder("API-28 root")
        child = folder("API-28 child", root["id"])
        got = ok(API, f"/folders/{child['id']}")
        eq(got["title"], "API-28 child", "get")
        renamed = ok(API, f"/folders/{child['id']}", "PUT", body={"title": "API-28 renamed"})
        eq(renamed["title"], "API-28 renamed", "rename")
        moved = ok(API, f"/folders/{child['id']}", "PUT", body={"parent_id": ""})
        eq(moved["parent_id"], "", "re-parent to root")
        eq(jrequest(API, f"/folders/{ZERO}")[0], 404, "get missing 404")
        ok(API, f"/folders/{child['id']}", "DELETE")
        eq(jrequest(API, f"/folders/{child['id']}")[0], 404, "delete then 404")

    @test("API-29", "folder tree")
    def _():
        root = folder("API-29 root")
        child = folder("API-29 child", root["id"])
        tree = ok(API, "/folders?as_tree=1")
        truthy(isinstance(tree, list), "tree is a list")
        node = next(f for f in tree if f["id"] == root["id"])
        truthy(any(c["id"] == child["id"] for c in node.get("children", [])),
               "child nested under parent")

    @test("API-30", "folder notes options")
    def _():
        f = folder("API-30 folder")
        a = note("API-30 alpha", parent_id=f["id"])
        note("API-30 beta", parent_id=f["id"])
        body = ok(API, f"/folders/{f['id']}/notes?limit=1&order_by=title&order_dir=asc&fields=id,title")
        eq(len(body["items"]), 1, "limit respected")
        eq(set(body["items"][0].keys()), {"id", "title"}, "fields")
        summary = ok(API, f"/folders/{f['id']}/notes?summary=1&limit=10")
        truthy(any(i["id"] == a["id"] for i in summary["items"]), "summary rows")
        truthy("body" not in summary["items"][0], "summary omits body")

    # ---------------------------------------------------------------- tags
    @test("API-31", "tags CRUD")
    def _():
        t = tag("API-31 tag")
        eq(ok(API, f"/tags/{t['id']}")["title"], "API-31 tag", "get")
        eq(ok(API, f"/tags/{t['id']}", "PUT", body={"title": "API-31 renamed"})["title"],
           "API-31 renamed", "rename")
        eq(jrequest(API, f"/tags/{ZERO}")[0], 404, "missing 404")
        ok(API, f"/tags/{t['id']}", "DELETE")
        eq(jrequest(API, f"/tags/{t['id']}")[0], 404, "delete then 404")

    @test("API-32", "attach tag is idempotent")
    def _():
        t = tag("API-32 tag")
        n = note("API-32 note")
        ok(API, f"/tags/{t['id']}/notes", "POST", body={"id": n["id"]})
        ok(API, f"/tags/{t['id']}/notes", "POST", body={"id": n["id"]})
        tags = ok(API, f"/notes/{n['id']}/tags?limit=10")["items"]
        eq(len(tags), 1, "single link after repeat")

    @test("API-33", "detach tag queues sync")
    def _():
        t = tag("API-33 tag")
        n = note("API-33 note", tag_ids=[t["id"]])
        before = max_event_id()
        ok(API, f"/tags/{t['id']}/notes/{n['id']}", "DELETE")
        eq(ok(API, f"/notes/{n['id']}/tags?limit=10")["items"], [], "link gone")
        events = events_since(before)
        truthy(any(e.get("item_type") == 6 and e.get("type") == 3 for e in events),
               "link deletion queued")

    @test("API-34", "note tags pagination and fields")
    def _():
        t1, t2 = tag("API-34 a"), tag("API-34 b")
        n = note("API-34 note", tag_ids=[t1["id"], t2["id"]])
        items = ok(API, f"/notes/{n['id']}/tags?limit=10")["items"]
        eq(len(items), 2, "both tags")
        proj = ok(API, f"/notes/{n['id']}/tags?limit=1&fields=id,title")
        eq(len(proj["items"]), 1, "limit")
        eq(set(proj["items"][0].keys()), {"id", "title"}, "fields")

    @test("API-35", "tag notes listing")
    def _():
        t = tag("API-35 tag")
        n = note("API-35 note", tag_ids=[t["id"]])
        body = ok(API, f"/tags/{t['id']}/notes?limit=10")
        truthy(any(i["id"] == n["id"] for i in body["items"]), "full notes")
        summary = ok(API, f"/tags/{t['id']}/notes?summary=1&limit=10")
        row = next(i for i in summary["items"] if i["id"] == n["id"])
        truthy("body" not in row, "summary omits body")

    # ---------------------------------------------------------------- search
    @test("API-36", "search notes")
    def _():
        n = note("API-36 searchable", body="unique exclusive xylophone token")
        body = ok(API, "/search?query=xylophone")
        truthy(any(i["id"] == n["id"] for i in body["items"]), "body match")
        title = ok(API, "/search?query=searchable%20unique")
        truthy(any(i["id"] == n["id"] for i in title["items"]), "title match")

    @test("API-37", "search folders")
    def _():
        f = folder("Zebra notebook API-37")
        body = ok(API, "/search?query=zebra&type=folder")
        truthy(any(i["id"] == f["id"] for i in body["items"]), "folder prefix match")
        body_lower = ok(API, "/search?query=ZEBRA&type=folder")
        truthy(any(i["id"] == f["id"] for i in body_lower["items"]), "case-insensitive")

    @test("API-38", "search tags")
    def _():
        t = tag("Quokka API-38")
        body = ok(API, "/search?query=quokka&type=tag")
        truthy(any(i["id"] == t["id"] for i in body["items"]), "tag prefix match")
        body_lower = ok(API, "/search?query=QUOKKA&type=tag")
        truthy(any(i["id"] == t["id"] for i in body_lower["items"]), "case-insensitive")

    @test("API-39", "search validation")
    def _():
        status, body, _ = jrequest(API, "/search")
        eq(status, 400, "query required")
        body = ok(API, f"/search?query=zzz-none-{ZERO}")
        eq(body["items"], [], "no matches is empty, not error")

    @test("API-40", "search tolerates FTS syntax")
    def _():
        for q in ['"unbalanced', "wild*", "(paren", "colon:term", "AND OR NOT", 'a"b*c(d)']:
            status, _, _ = jrequest(API, "/search?query=" + urllib.parse.quote(q))
            truthy(status < 500, f"query {q!r} -> {status}")

    # ---------------------------------------------------------------- resources
    @test("API-41", "resource upload and download")
    def _():
        r = upload_resource("QA resource", "hello.txt", b"hello resource", "text/plain")
        created_resources.append(r["id"])
        eq(r["filename"], "hello.txt", "filename")
        truthy("text/plain" in r["mime"], "detected mime")
        status, raw, hdrs = request(API, f"/resources/{r['id']}/file")
        eq(status, 200, "file status")
        eq(raw, b"hello resource", "file bytes")
        truthy("text/plain" in hdrs.get("content-type", ""), "file content type")

    @test("API-42", "resource update and delete")
    def _():
        r = upload_resource("API-42", "a.bin", b"\x00\x01\x02", "application/octet-stream")
        created_resources.append(r["id"])
        updated = ok(API, f"/resources/{r['id']}", "PUT",
                     body={"title": "API-42 renamed", "filename": "b.bin", "mime": "text/x-q"})
        eq(updated["title"], "API-42 renamed", "title")
        eq(updated["filename"], "b.bin", "filename")
        ok(API, f"/resources/{r['id']}", "DELETE")
        eq(jrequest(API, f"/resources/{r['id']}")[0], 404, "gone")

    @test("API-43", "read-only policy covers resources")
    def _():
        status, body, _ = jrequest(RO, "/resources", "POST",
                                   body={"title": "nope"}, token=RO_TOKEN)
        truthy(status == 403, f"read-only resource create refused, got {status}: {body}")

    # ---------------------------------------------------------------- revisions
    @test("API-44", "revisions")
    def _():
        body = ok(API, "/revisions?limit=10")
        truthy("items" in body, "list shape")
        eq(jrequest(API, f"/revisions/{ZERO}")[0], 404, "missing revision 404")
        eq(jrequest(API, f"/revisions/{ZERO}", "DELETE")[0], 404, "delete missing 404")

    # ---------------------------------------------------------------- events
    @test("API-45", "events feed")
    def _():
        before = max_event_id()
        n = note("API-45 event source")
        events = events_since(before)
        truthy(any(e.get("item_id") == n["id"] for e in events), "create recorded")
        first = ok(API, f"/events?limit=1&cursor={before}")
        truthy(first.get("has_more") in (True, False), "has_more present")
        ev = next(e for e in events if e.get("item_id") == n["id"])
        one = ok(API, f"/events/{ev['id']}")
        eq(one["id"], ev["id"], "single event")
        eq(jrequest(API, "/events/999999999")[0], 404, "missing event 404")

    # ---------------------------------------------------------------- transport
    @test("API-46", "CORS preflight")
    def _():
        for method in ("GET", "POST", "PUT", "DELETE"):
            status, _, hdrs = request(API, "/notes", "OPTIONS", token=None, headers={
                "Origin": "app://localhost",
                "Access-Control-Request-Method": method,
                "Access-Control-Request-Headers": "content-type, authorization",
            })
            truthy(status in (200, 204), f"{method} preflight status {status}")
            truthy("*" in hdrs.get("access-control-allow-origin", ""), f"{method} allow-origin")
            truthy(method in hdrs.get("access-control-allow-methods", ""), f"{method} allowed")
            allowed_headers = hdrs.get("access-control-allow-headers", "").lower()
            truthy("content-type" in allowed_headers and "authorization" in allowed_headers,
                   f"{method} allow-headers")

    @test("API-47", "bad JSON rejected")
    def _():
        status, body, _ = jrequest(API, "/notes", "POST", data=b"{not json")
        eq(status, 400, "create bad json")
        truthy("invalid JSON" in json.dumps(body), "create error text")
        n = note("API-47 note")
        status, body, _ = jrequest(API, f"/notes/{n['id']}", "PUT", data=b"{not json")
        eq(status, 400, "update bad json")
        eq(ok(API, f"/notes/{n['id']}")["title"], "API-47 note", "unchanged")

    # ---------------------------------------------------------------- policy
    @test("API-48", "read-only policy")
    def _():
        for path, method, payload in [
            ("/notes", "POST", {"title": "ro"}),
            ("/folders", "POST", {"title": "ro"}),
            ("/tags", "POST", {"title": "ro"}),
        ]:
            status, body, _ = jrequest(RO, path, method, body=payload, token=RO_TOKEN)
            eq(status, 403, f"{method} {path} refused")
            truthy("disabled" in json.dumps(body) or "read-only" in json.dumps(body),
                   f"{method} {path} hint")
        seed = next(n for n in ok(RO, "/notes?limit=100", token=RO_TOKEN)["items"]
                    if n["title"] == "RO seed note")
        status, _, _ = jrequest(RO, f"/notes/{seed['id']}", "PUT",
                                body={"title": "changed"}, token=RO_TOKEN)
        eq(status, 403, "update refused")
        eq(ok(RO, f"/notes/{seed['id']}", token=RO_TOKEN)["title"], "RO seed note", "unchanged")

    @test("API-49", "writable allow-list")
    def _():
        folders = ok(ALLOW, "/folders?limit=100", token=ALLOW_TOKEN)["items"]
        allowed = next(f for f in folders if f["title"] == "Allowed")
        other = next(f for f in folders if f["title"] == "Other")
        tags = ok(ALLOW, "/tags?limit=100", token=ALLOW_TOKEN)["items"]
        green = next(t for t in tags if t["title"] == "green")
        red = next(t for t in tags if t["title"] == "red")

        made = ok(ALLOW, "/notes", "POST",
                  body={"title": "API-49 allowed", "parent_id": allowed["id"]}, token=ALLOW_TOKEN)
        request(ALLOW, f"/notes/{made['id']}", "DELETE", token=ALLOW_TOKEN)
        status, _, _ = jrequest(ALLOW, "/notes", "POST",
                                body={"title": "API-49 other", "parent_id": other["id"]},
                                token=ALLOW_TOKEN)
        eq(status, 403, "non-listed folder refused")
        status, _, _ = jrequest(ALLOW, "/notes", "POST", body={"title": "API-49 root"},
                                token=ALLOW_TOKEN)
        eq(status, 403, "root refused")

        made2 = ok(ALLOW, "/notes", "POST",
                   body={"title": "API-49 tagged", "parent_id": allowed["id"]}, token=ALLOW_TOKEN)
        ok(ALLOW, f"/tags/{green['id']}/notes", "POST", body={"id": made2["id"]}, token=ALLOW_TOKEN)
        status, _, _ = jrequest(ALLOW, f"/tags/{red['id']}/notes", "POST",
                                body={"id": made2["id"]}, token=ALLOW_TOKEN)
        eq(status, 403, "non-listed tag refused")

        # A move must obey the target folder's policy, not just the source's.
        status, _, _ = jrequest(ALLOW, f"/notes/{made2['id']}", "PUT",
                                body={"parent_id": other["id"]}, token=ALLOW_TOKEN)
        eq(status, 403, "move to non-listed folder refused")
        status, _, _ = jrequest(ALLOW, f"/notes/{made2['id']}", "PUT",
                                body={"parent_id": ""}, token=ALLOW_TOKEN)
        eq(status, 403, "move to root refused")
        eq(ok(ALLOW, f"/notes/{made2['id']}", token=ALLOW_TOKEN)["parent_id"],
           allowed["id"], "note not moved")
        request(ALLOW, f"/notes/{made2['id']}", "DELETE", token=ALLOW_TOKEN)

    @test("API-50", "orphan note guard")
    def _():
        status, body, _ = jrequest(API, "/notes", "POST",
                                   body={"title": "API-50 orphan", "parent_id": ZERO})
        eq(status, 400, "create into missing folder refused")
        # A bad parent is a 400 even under an allow-list that would otherwise
        # refuse the write as read-only.
        status, body, _ = jrequest(ALLOW, "/notes", "POST",
                                   body={"title": "API-50 orphan", "parent_id": ZERO},
                                   token=ALLOW_TOKEN)
        eq(status, 400, "orphan refused before the allow-list check")
        n = note("API-50 movable")
        status, body, _ = jrequest(API, f"/notes/{n['id']}", "PUT", body={"parent_id": ZERO})
        eq(status, 400, "move into missing folder refused")
        eq(ok(API, f"/notes/{n['id']}")["parent_id"], "", "note not moved")

    # ---------------------------------------------------------------- data integrity
    @test("API-51", "unicode round-trip")
    def _():
        body = "日本語 — café 🙂\nπόλη\n\U0001F1E6\U0001F1E7 combining: e\u0301\n" + "字" * 2000
        n = note("API-51 unicode 🙂 café", body=body)
        eq(ok(API, f"/notes/{n['id']}")["body"], body, "create/get round-trip")
        updated = ok(API, f"/notes/{n['id']}", "PUT", body={"body": body + "\n追加 🙂"})
        eq(updated["body"], body + "\n追加 🙂", "update round-trip")
        eq(ok(API, f"/notes/{n['id']}")["title"], "API-51 unicode 🙂 café", "title round-trip")

    @test("API-52", "unknown routes and methods")
    def _():
        eq(jrequest(API, "/definitely-not-a-route")[0], 404, "unknown path")
        eq(request(API, "/notes", "DELETE")[0], 405, "unsupported method")

    # ---------------------------------------------------------------- run
    for story_id, name, fn in tests:
        if only and story_id not in only:
            continue
        try:
            fn()
            results[story_id] = {"ok": True, "name": name, "detail": ""}
            print(f"ok   {story_id} {name}")
        except Exception as e:  # noqa: BLE001
            results[story_id] = {"ok": False, "name": name, "detail": str(e)}
            print(f"FAIL {story_id} {name}: {e}")
        finally:
            cleanup()

    RESULTS.write_text(json.dumps({
        "at": __import__("datetime").datetime.now(__import__("datetime").timezone.utc).isoformat(),
        "base": API, "results": results,
    }, indent=2, ensure_ascii=False))
    passed = sum(1 for r in results.values() if r["ok"])
    failed = len(results) - passed
    print(f"\n{passed} passed, {failed} failed, {len(results)} run")
    return 1 if failed else 0


def max_event_id():
    """Highest event id currently visible, by paging the feed to its end."""
    cursor = 0
    last = 0
    for _ in range(100):
        body = ok(API, f"/events?limit=100&cursor={cursor}")
        if not body["items"]:
            break
        last = body["items"][-1]["id"]
        if not body.get("has_more"):
            break
        cursor = last
    return last


def events_since(cursor):
    """Every event recorded after the given id."""
    out = []
    for _ in range(100):
        body = ok(API, f"/events?limit=100&cursor={cursor}")
        out.extend(body["items"])
        if not body.get("has_more") or not body["items"]:
            break
        cursor = body["items"][-1]["id"]
    return out


def upload_resource(title, filename, content, ctype):
    boundary = "----QA" + str(abs(hash((title, filename))) % 10**10)
    parts = []
    parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="props"\r\n\r\n'
                 + json.dumps({"title": title}) + "\r\n")
    parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="data"; '
                 f'filename="{filename}"\r\nContent-Type: {ctype}\r\n\r\n')
    body = parts[0].encode() + parts[1].encode() + content + f"\r\n--{boundary}--\r\n".encode()
    status, raw, _ = request(API, "/resources", "POST", data=body,
                             headers={"Content-Type": f"multipart/form-data; boundary={boundary}"})
    assert status == 200, f"upload -> {status}: {raw[:200]}"
    return json.loads(raw)


def run_go_test(name):
    proc = subprocess.run(
        ["go", "test", "-run", name, "-count=1", "./internal/clipper/"],
        cwd=ROOT, capture_output=True, text=True,
    )
    assert proc.returncode == 0, f"go test {name} failed:\n{proc.stdout}\n{proc.stderr}"


if __name__ == "__main__":
    sys.exit(main())
