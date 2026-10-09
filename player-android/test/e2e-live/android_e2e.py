#!/usr/bin/env python3
"""End-to-end run of the Player Android app on an emulator (adb + uiautomator)
against a deployed server that holds the generated all-formats test library
(player-server/testdata/gen-all-formats.sh).

Usage: android_e2e.py <base-url>      e.g. https://player.example.org

Credentials are read from the env file named by PLAYER_E2E_ENV (default
~/.config/player-e2e.env) with the keys E2E_ADMIN_USER, E2E_ADMIN_PASS,
E2E_USER and E2E_USER_PASS. Results go to results-<host>.json and
screenshots to shots-<host>/ below PLAYER_E2E_OUT (default: the current
directory). See README.md.
"""
import json, os, re, sys, time, urllib.parse, urllib.request, urllib.error, http.cookiejar
from PIL import Image, ImageChops
import adrv

BASE = sys.argv[1].rstrip("/")
HOST = urllib.parse.urlparse(BASE).hostname
PKG = "zone.foo.player_android"
ENV_FILE = os.environ.get("PLAYER_E2E_ENV", os.path.expanduser("~/.config/player-e2e.env"))
ENV = dict(line.strip().split("=", 1) for line in open(ENV_FILE) if "=" in line and not line.startswith("#"))
OUT = os.environ.get("PLAYER_E2E_OUT", os.getcwd())
SHOTS = os.path.join(OUT, f"shots-{HOST}")

def _pubspec_version():
    """The app version this checkout would release (pubspec `version:` without the build number)."""
    path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "pubspec.yaml")
    match = re.search(r"^version:\s*([0-9.]+)", open(path).read(), re.M)
    return match.group(1) if match else ""

# The version the installed APK must show in Settings; override when testing
# an APK that was not built from this checkout.
APP_VERSION = os.environ.get("PLAYER_E2E_APP_VERSION") or _pubspec_version()
os.makedirs(SHOTS, exist_ok=True)

# One sample-<ext>.<ext> per extension in player-server/internal/mediatype.
FORMATS = {
    "test-videos": ["mp4", "mkv", "avi", "mov", "wmv", "flv", "webm"],
    "test-audio": ["mp3", "wav", "flac", "aac", "ogg", "m4a", "wma", "m4b", "opus"],
    "test-images": ["jpg", "jpeg", "png", "gif", "webp", "bmp", "avif", "svg"],
}
RESULTS = []

def record(name, ok, detail=""):
    RESULTS.append(dict(name=name, ok=bool(ok), detail=detail))
    print(("PASS " if ok else "FAIL ") + name + (f"  [{detail}]" if detail else ""), flush=True)

# ---------------------------------------------------------------- API side
class API:
    def __init__(self, user, password):
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.call("POST", "/api/v1/auth/login", {"username": user, "password": password})

    def call(self, method, path, body=None, raw=False):
        req = urllib.request.Request(BASE + path, method=method,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers={"Content-Type": "application/json"})
        try:
            with self.opener.open(req, timeout=30) as res:
                data = res.read()
                return (res.status, data) if raw else (json.loads(data) if data.strip() else None)
        except urllib.error.HTTPError as err:
            if raw: return (err.code, b"")
            raise

def anon_status(path):
    try:
        with urllib.request.urlopen(BASE + path, timeout=30) as res: return res.status
    except urllib.error.HTTPError as err: return err.code

# ---------------------------------------------------------------- UI helpers
def back(n=1):
    for _ in range(n):
        adrv.adb("shell", "input", "keyevent", "KEYCODE_BACK"); time.sleep(1.3)

def labels():
    return [adrv.label(n) for n in adrv.nodes() if adrv.label(n)]

def shot(name):
    path = os.path.join(SHOTS, name + ".png")
    adrv.shot(path)
    return path

def clear_field(n=45):
    adrv.adb("shell", "input", "keyevent", "KEYCODE_MOVE_END")
    adrv.adb("shell", "input", "keyevent", *["KEYCODE_DEL"] * n)

def swipe(up=True):
    a, b = ("1700", "700") if up else ("700", "1700")
    adrv.adb("shell", "input", "swipe", "540", a, "540", b, "400"); time.sleep(1.2)

def find_scrolling(pattern):
    """Find a grid item, scrolling the grid when it is off screen."""
    for move in (None, True, True, False, False, False):
        if move is not None: swipe(up=move)
        node = adrv.find(pattern)
        # Items clipped by the bottom edge are tappable but only just; nudge.
        if node and node["b"][3] - node["b"][1] > 150: return node
    return None

def logcat_errors():
    out = adrv.adb("logcat", "-d")
    hits = [l for l in out.splitlines() if re.search(r"ExoPlaybackException|PlaybackException|Source error|UnrecognizedInputFormat|Decoder (init )?failed|Exception loading image|ImageCodecException|Failed to decode", l)]
    return hits

def region_stats(path, box):
    """Fraction of pixels in box that differ clearly from the box corner colour."""
    img = Image.open(path).convert("RGB").crop(box)
    small = img.resize((108, 180))
    ref = small.getpixel((1, 1))
    px = list(small.get_flattened_data())
    return sum(1 for p in px if max(abs(p[i] - ref[i]) for i in range(3)) > 30) / len(px)

def frames_differ(a, b, box):
    diff = ImageChops.difference(Image.open(a).convert("RGB").crop(box), Image.open(b).convert("RGB").crop(box))
    return diff.getbbox() is not None

def ui_error_text():
    hits = [l for l in labels() if re.search(r"(?i)error|failed|unsupported|cannot|could not|unable", l)]
    return " | ".join(hits)[:200]

def foreground():
    return PKG in adrv.adb("shell", "dumpsys", "activity", "activities").split("topResumedActivity", 1)[-1][:200]

def go_home():
    """Return to the Library root without ever backing out of the app."""
    adrv.adb("shell", "cmd", "statusbar", "collapse")
    for _ in range(8):
        if not foreground():
            adrv.adb("shell", "monkey", "-p", PKG, "-c", "android.intent.category.LAUNCHER", "1"); time.sleep(4)
        if "Open navigation menu" in labels(): return True
        back()
    return False

def ensure_grid(set_name):
    """Navigate back until the given set's grid is on screen."""
    for _ in range(5):
        current = labels()
        if set_name in current and "Browse folders" in current: return True
        if "Library" in current and "Open navigation menu" in current:
            adrv.tap(f"^{set_name}$"); time.sleep(3); continue
        if not foreground(): go_home(); continue
        back()
    return False

def open_detail(set_name, file_name):
    if not ensure_grid(set_name): return False
    node = find_scrolling("^" + re.escape(file_name))
    if not node: return False
    adrv.tap_node(node); time.sleep(3)
    return adrv.wait(r"^(Play Video|Play Audio|View Image)$", timeout=10) is not None

# ---------------------------------------------------------------- journeys
def connect_and_login(user, password, check_wrong_password=False):
    adrv.adb("shell", "am", "force-stop", PKG)
    adrv.adb("shell", "pm", "clear", PKG)
    adrv.adb("shell", "monkey", "-p", PKG, "-c", "android.intent.category.LAUNCHER", "1")
    ok = adrv.wait("^Connect to server$", timeout=40)
    record("app starts in local mode with a Connect to server action", ok)
    adrv.tap("^Connect to server$"); time.sleep(2)
    adrv.tap(".", cls="EditText"); time.sleep(1)
    clear_field(); adrv.type_text(BASE); time.sleep(1)
    adrv.tap("^Save URL$")
    record("saving the server URL leads to Sign In", adrv.wait("^Welcome back$", timeout=25))
    fields = [n for n in adrv.nodes() if n["cls"] == "EditText"]
    adrv.tap_node(fields[0]); time.sleep(1); adrv.type_text(user)
    if check_wrong_password:
        adrv.tap_node(fields[1]); time.sleep(1); adrv.type_text("definitely-wrong-pw")
        adrv.adb("shell", "input", "keyevent", "KEYCODE_ENTER")
        seen = ""
        for _ in range(4):
            seen = ui_error_text() or " | ".join(l for l in labels() if re.search(r"(?i)invalid|incorrect|credential", l))
            if seen: break
        shot("login-wrong-password")
        still_login = adrv.find("^Welcome back$") is not None
        record("wrong password is rejected with a visible message", bool(seen) and still_login, seen or "no error text found in UI")
        fields = [n for n in adrv.nodes() if n["cls"] == "EditText"]
        adrv.tap_node(fields[1]); time.sleep(1); clear_field(25)
    else:
        adrv.tap_node(fields[1]); time.sleep(1)
    adrv.type_text(password)
    adrv.adb("shell", "input", "keyevent", "KEYCODE_ENTER"); time.sleep(2)
    if adrv.find("^Welcome back$"):
        adrv.adb("shell", "input", "keyevent", "KEYCODE_BACK"); time.sleep(1)
        adrv.tap("^Sign In$", cls="Button")
    return adrv.wait("^Library$", timeout=30) is not None

def test_library_listing():
    found = labels()
    record("library shows the three test sets", all(s in found for s in FORMATS), ", ".join(s for s in FORMATS if s in found))
    shot("library")
    for set_name, exts in FORMATS.items():
        ensure_grid(set_name)
        seen = set()
        for move in (None, True, True):
            if move: swipe()
            seen |= {l.split(" / ")[0] for l in labels() if l.startswith("sample-")}
        swipe(up=False); swipe(up=False)
        missing = [f"sample-{e}.{e}" for e in exts if f"sample-{e}.{e}" not in seen]
        shot(f"grid-{set_name}")
        record(f"{set_name} lists all {len(exts)} files", not missing, "missing: " + ", ".join(missing) if missing else "")

# File names the server flags as "transcoded": the app plays a converted
# copy and shows "Preparing playback…" until the server has it ready.
TRANSCODED = set()

# Filled in main(): the admin API client and media ids by file name.
SERVER = {"api": None, "ids": {}}

def reset_progress(name):
    """Forget saved progress, so playback starts at 0:00.

    Progress left by an earlier run would resume these short samples near
    their end; the clip then finishes before the screenshots are taken.
    """
    media_id = SERVER["ids"].get(name)
    if SERVER["api"] and media_id:
        SERVER["api"].call("POST", "/api/v1/progress/status", {"media_id": media_id, "status": "not_started"}, raw=True)

def start_playback(button, name, settle):
    """Tap the play button and return once playback should be running.

    A plain file gets a fixed settle time. A transcoded one may first wait
    for the server, so the "Preparing playback…" label is polled away (each
    poll is a slow UI dump, which is why plain files skip it: their short
    samples would be over before the screenshots are taken).
    """
    reset_progress(name)
    adrv.tap(button)
    if name not in TRANSCODED:
        time.sleep(settle); return
    time.sleep(1.5)
    deadline = time.time() + 180
    while time.time() < deadline and any("Preparing playback" in l for l in labels()):
        time.sleep(1)

def test_video(ext):
    name = f"sample-{ext}.{ext}"
    if not open_detail("test-videos", name): return record(f"video .{ext} plays", False, "could not open detail screen")
    adrv.adb("logcat", "-c")
    start_playback("^Play Video$", name, settle=4)
    box = (0, 230, 1080, 2100)
    a = shot(f"video-{ext}-a"); time.sleep(1.0); b = shot(f"video-{ext}-b")
    filled, moving = region_stats(a, box), frames_differ(a, b, box)
    errors, text = logcat_errors(), ui_error_text()
    ok = filled > 0.10 and moving and not text
    detail = f"picture={filled:.0%} moving={moving}"
    if text: detail += f" ui='{text}'"
    if errors: detail += " logcat=" + re.sub(r"^.*?: ", "", errors[-1])[:140]
    record(f"video .{ext} plays", ok, detail)

def test_audio(ext):
    name = f"sample-{ext}.{ext}"
    if not open_detail("test-audio", name): return record(f"audio .{ext} plays", False, "could not open detail screen")
    adrv.adb("logcat", "-c")
    start_playback("^Play Audio$", name, settle=5)
    bar = (100, 1230, 980, 1420)
    a = shot(f"audio-{ext}-a"); time.sleep(2.5); b = shot(f"audio-{ext}-b")
    advancing = frames_differ(a, b, bar)
    errors, text = logcat_errors(), ui_error_text()
    times = [l for l in labels() if re.fullmatch(r"\d\d:\d\d", l)]
    total = times[-1] if times else ""
    ok = advancing and total == "00:12" and not text
    detail = f"seekbar advancing={advancing} total={total}"
    if text: detail += f" ui='{text}'"
    if errors: detail += " logcat=" + re.sub(r"^.*?: ", "", errors[-1])[:140]
    record(f"audio .{ext} plays", ok, detail)

def test_image(ext):
    name = f"sample-{ext}.{ext}"
    if not open_detail("test-images", name): return record(f"image .{ext} opens", False, "could not open detail screen")
    adrv.adb("logcat", "-c")
    adrv.tap("^View Image$"); time.sleep(4)
    path = shot(f"image-{ext}")
    filled = region_stats(path, (0, 230, 1080, 2100))
    errors, text = logcat_errors(), ui_error_text()
    detail = f"picture={filled:.0%}"
    if text: detail += f" ui='{text}'"
    if errors: detail += " logcat=" + re.sub(r"^.*?: ", "", errors[-1])[:140]
    record(f"image .{ext} opens", filled > 0.15 and not text, detail)

def test_search_and_filter():
    ensure_grid("test-videos")
    adrv.tap(".*", cls="EditText"); time.sleep(1); adrv.type_text("webm"); time.sleep(3)
    items = [l for l in labels() if l.startswith("sample-")]
    record("search narrows the grid to the matching file", len(items) == 1 and items[0].startswith("sample-webm"), ", ".join(items))
    clear_field(8); adrv.adb("shell", "input", "keyevent", "KEYCODE_BACK"); time.sleep(2)
    adrv.tap("^Audio$"); time.sleep(3)
    items = [l for l in labels() if l.startswith("sample-")]
    record("type filter Audio hides the videos", len(items) == 0, ", ".join(items))
    adrv.tap("^All$"); time.sleep(2)

def test_favourite(api, media):
    item = media["sample-mp3.mp3"]
    if not open_detail("test-audio", "sample-mp3.mp3"): return record("favourite toggles", False, "detail not opened")
    adrv.tap("^Add to favourites$"); time.sleep(2)
    in_ui = adrv.wait("^Remove from favourites$", timeout=8) is not None
    on_server = item["id"] in [m["id"] for m in api.call("GET", "/api/v1/media?favorites=true") or []]
    back(); ensure_grid("test-audio")
    adrv.tap("^Show favourites only$"); time.sleep(3)
    only = [l.split(" / ")[0] for l in labels() if l.startswith("sample-")]
    record("favourite is set from the app and filters the grid", in_ui and on_server and only == ["sample-mp3.mp3"], f"ui={in_ui} server={on_server} filtered={only}")
    adrv.tap("favourites") ; time.sleep(2)
    if not open_detail("test-audio", "sample-mp3.mp3"):
        return record("favourite can be removed again", False, "detail not opened")
    adrv.tap("^Remove from favourites$")
    # The app's request may still be on its way when the tap returns.
    off, deadline = False, time.time() + 15
    while not off and time.time() < deadline:
        time.sleep(1)
        off = item["id"] not in [m["id"] for m in api.call("GET", "/api/v1/media?favorites=true") or []]
    record("favourite can be removed again", off)
    back()

def test_tag(api, media):
    item, tag = media["sample-png.png"], f"droid{int(time.time()) % 100000}"
    if not open_detail("test-images", "sample-png.png"): return record("tag add", False, "detail not opened")
    adrv.tap(".*", cls="EditText"); time.sleep(1); adrv.type_text(tag); time.sleep(1)
    adrv.tap("^Add tag$"); time.sleep(3)
    adrv.adb("shell", "input", "keyevent", "KEYCODE_BACK") if not adrv.find("^View Image$") else None
    in_ui = adrv.wait(tag, timeout=6) is not None
    detail = api.call("GET", f"/api/v1/media/{item['id']}")
    on_server = tag in json.dumps(detail)
    shot("tag-added")
    record("tag added from the app is stored on the server", in_ui and on_server, f"ui={in_ui} server={on_server}")
    api.call("DELETE", f"/api/v1/media/{item['id']}/tags/{tag}", raw=True)
    back()

def test_notes(api, media):
    item, text = media["sample-flac.flac"], f"droidnote{int(time.time()) % 100000}"
    if not open_detail("test-audio", "sample-flac.flac"): return record("notes", False, "detail not opened")
    adrv.tap("^Show menu$"); time.sleep(1.5); adrv.tap("^Notes$"); time.sleep(3)
    adrv.tap(".*", cls="EditText"); time.sleep(1); adrv.type_text(text); time.sleep(1)
    adrv.tap("^Show menu$"); time.sleep(1.5)
    menu = labels(); print("   notes menu:", menu, flush=True)
    if not adrv.tap("(?i)^save", timeout=3): back()
    time.sleep(2); back(); time.sleep(2)
    status, body = api.call("GET", f"/api/v1/media/{item['id']}/notes", raw=True)
    record("note written in the app is stored on the server", status == 200 and text in body.decode(), f"HTTP {status}")
    api.call("DELETE", f"/api/v1/media/{item['id']}/notes", raw=True)
    ensure_grid("test-audio")

def test_share(api, media):
    item = media["sample-mp4.mp4"]
    if not open_detail("test-videos", "sample-mp4.mp4"): return record("share", False, "detail not opened")
    adrv.tap("^Show menu$"); time.sleep(1.5); adrv.tap("^Share$"); time.sleep(2)
    adrv.tap("^Share$", cls="Button"); time.sleep(4)
    after = labels(); print("   after share:", after[:12], flush=True); shot("share-created")
    shares = api.call("GET", f"/api/v1/media/{item['id']}/shares") or []
    token = shares[-1]["token"] if shares else ""
    public = anon_status(f"/s/{token}/stream") if token else 0
    record("share link created in the app works without a session", bool(token) and public == 200, f"shares={len(shares)} anonymous stream HTTP {public}")
    go_home()
    adrv.tap("^Open navigation menu$"); time.sleep(1.5); adrv.tap("^My Shares$"); time.sleep(4)
    listed = any("sample-mp4" in l for l in labels()); shot("my-shares")
    record("My Shares lists the new share", listed)
    for share in shares: api.call("DELETE", f"/api/v1/shares/{share['token']}", raw=True)
    record("revoked share stops working", token and anon_status(f"/s/{token}/stream") >= 400)
    back()

def anon_get(path, headers=None):
    """Anonymous GET without a cookie jar; returns (status, body)."""
    req = urllib.request.Request(BASE + path, headers=headers or {})
    try:
        with urllib.request.urlopen(req, timeout=30) as res: return res.status, res.read()
    except urllib.error.HTTPError as err: return err.code, b""

def test_single_use_share(api, media):
    """A max_uses=1 share serves one whole viewing, not one HTTP request.

    Server-side only, acting as the app's share viewer does: this script has
    no way to open a share link inside the app (it drives the UI by taps and
    never sends a VIEW intent). The app fetches the share JSON once, which
    opens the viewing, and then plays the JSON's playback_url; that URL
    carries the viewing credential (?view=), so the player's ranged requests
    cost no further use. Any client without the credential is turned away.
    """
    item = media["sample-mp4.mp4"]
    status, body = api.call("POST", f"/api/v1/media/{item['id']}/shares", {"max_uses": 1}, raw=True)
    token = json.loads(body).get("token", "") if status == 200 else ""
    if not token: return record("single-use share", False, f"create share HTTP {status}")
    try:
        status, body = anon_get(f"/s/{token}", {"Accept": "application/json"})
        meta = json.loads(body) if status == 200 else {}
        url = meta.get("playback_url", "")
        record("single-use share: the metadata fetch opens a viewing with a credential",
               status == 200 and bool(meta.get("view")) and url == f"/s/{token}/stream?view={meta.get('view')}", f"HTTP {status}")
        ranges = [anon_get(url, {"Range": r})[0] for r in ("bytes=0-1023", "bytes=1024-4095", "bytes=0-")] if url else []
        record("single-use share: three ranged requests of the viewing are all served", ranges == [206, 206, 206], f"HTTP {ranges}")
        second, _ = anon_get(f"/s/{token}", {"Accept": "application/json"})
        direct = anon_status(f"/s/{token}/stream")
        record("single-use share: a second client is refused", second == 410 and direct == 410, f"metadata HTTP {second}, stream HTTP {direct}")
        used = next((s["used_count"] for s in api.call("GET", "/api/v1/shares") or [] if s["token"] == token), None)
        record("single-use share: exactly one use was counted", used == 1, f"used_count={used}")
    finally:
        api.call("DELETE", f"/api/v1/shares/{token}", raw=True)

def test_progress(api, media):
    item = media["sample-mkv.mkv"]
    api.call("POST", "/api/v1/progress/status", {"media_id": item["id"], "status": "not_started"}, raw=True)
    if not open_detail("test-videos", "sample-mkv.mkv"): return record("progress", False, "detail not opened")
    adrv.tap("^Play Video$"); time.sleep(6)
    back(); time.sleep(3)
    detail = api.call("GET", f"/api/v1/media/{item['id']}") or {}
    position = (detail.get("progress") or {}).get("position_seconds") or 0
    record("playback position is saved to the server when leaving the player", position > 1, f"position_seconds={position}")
    api.call("POST", "/api/v1/progress/status", {"media_id": item["id"], "status": "not_started"}, raw=True)
    back()

def open_settings():
    go_home()
    adrv.tap("^Settings$"); time.sleep(3)

def test_settings_admin_logout(expect_admin):
    open_settings()
    top = labels()
    # The admin's Settings page is longer than one swipe; read it in pages.
    swipe(); middle = labels(); shot("settings-admin" if expect_admin else "settings-user")
    swipe(); bottom = labels()
    both = top + middle + bottom
    record("Settings shows the signed-in account", any(l == (ENV["E2E_ADMIN_USER"] if expect_admin else ENV["E2E_USER"]) for l in both))
    record(f"Settings shows app version {APP_VERSION}", any(f"Version {APP_VERSION}" in l for l in both),
           next((l for l in both if "Version" in l), ""))
    has_admin = any(l.startswith("Manage Users") for l in both)
    record("administration section is %s" % ("shown to the admin" if expect_admin else "hidden from a regular user"), has_admin == expect_admin)
    if expect_admin:
        adrv.tap("^Manage Users"); time.sleep(4)
        users = labels(); shot("admin-users")
        record("Manage Users lists the accounts", any(ENV["E2E_USER"] in l for l in users) and any(ENV["E2E_ADMIN_USER"] in l for l in users))
        back()
    swipe(up=False); swipe(up=False); time.sleep(1)
    adrv.tap("^Log Out$"); time.sleep(2)
    adrv.tap("(?i)^(log out|confirm|yes|ok)$", timeout=3)
    record("Log Out returns to the sign-in screen", adrv.wait("^Welcome back$", timeout=20) is not None)

def test_regular_user(api):
    users = api.call("GET", "/api/v1/admin/users")
    sets = api.call("GET", "/api/v1/sets")
    grant = {"set_id": next(s["id"] for s in sets if s["name"] == "test-images"),
             "user_id": next(u["id"] for u in users if u["username"] == ENV["E2E_USER"])}
    api.call("POST", "/api/v1/admin/permissions", {**grant, "role": "viewer"}, raw=True)
    try:
        ok = connect_and_login(ENV["E2E_USER"], ENV["E2E_USER_PASS"])
        record("regular user can sign in", ok)
        time.sleep(2); found = labels(); shot("library-regular-user")
        visible = [s for s in FORMATS if s in found]
        record("regular user sees only the granted set", visible == ["test-images"], ", ".join(visible))
        test_image("jpg")
        test_settings_admin_logout(expect_admin=False)
    finally:
        api.call("DELETE", "/api/v1/admin/permissions", grant, raw=True)

def main():
    api = API(ENV["E2E_ADMIN_USER"], ENV["E2E_ADMIN_PASS"])
    media = {m["file_name"]: m for m in api.call("GET", "/api/v1/media?limit=500")}
    TRANSCODED.update(name for name, m in media.items() if m.get("transcoded"))
    SERVER.update(api=api, ids={name: m["id"] for name, m in media.items()})
    record("admin can sign in", connect_and_login(ENV["E2E_ADMIN_USER"], ENV["E2E_ADMIN_PASS"], check_wrong_password=True))
    test_library_listing()
    for ext in FORMATS["test-videos"]: test_video(ext)
    for ext in FORMATS["test-audio"]: test_audio(ext)
    for ext in FORMATS["test-images"]: test_image(ext)
    for step in (test_search_and_filter, lambda: test_favourite(api, media), lambda: test_tag(api, media),
                 lambda: test_notes(api, media), lambda: test_share(api, media), lambda: test_single_use_share(api, media),
                 lambda: test_progress(api, media),
                 lambda: test_settings_admin_logout(expect_admin=True), lambda: test_regular_user(api)):
        try: step()
        except Exception as err: record(f"step crashed: {getattr(step, '__name__', 'step')}", False, repr(err)[:200])
    json.dump(RESULTS, open(os.path.join(OUT, f"results-{HOST}.json"), "w"), indent=1)
    failed = [r for r in RESULTS if not r["ok"]]
    print(f"\n{len(RESULTS) - len(failed)} passed, {len(failed)} failed", flush=True)

if __name__ == "__main__":
    main()
