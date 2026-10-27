"""Reusable steps for driving the Player Android app through adb/uiautomator.

Shared by android_e2e.py (all-formats run against a deployed server) and
music_e2e.py (music fixture run). Steps find controls by their visible
label or content description (Flutter semantics), never by coordinates,
except swipe(), which scrolls the middle of a 1080 px wide screen.

Checks are reported through a `record(name, ok, detail)` callable and
screenshots through a `shot(name)` callable supplied by the runner, so each
runner keeps its own result format.
"""
import re
import time

import adrv

PKG = "zone.foo.player_android"


def back(n=1):
    for _ in range(n):
        adrv.adb("shell", "input", "keyevent", "KEYCODE_BACK"); time.sleep(1.3)


def labels():
    return [adrv.label(n) for n in adrv.nodes() if adrv.label(n)]


def clear_field(n=45):
    adrv.adb("shell", "input", "keyevent", "KEYCODE_MOVE_END")
    adrv.adb("shell", "input", "keyevent", *["KEYCODE_DEL"] * n)


def swipe(up=True):
    a, b = ("1700", "700") if up else ("700", "1700")
    adrv.adb("shell", "input", "swipe", "540", a, "540", b, "400"); time.sleep(1.2)


def find_scrolling(pattern):
    """Find a grid item, scrolling the grid when it is off screen."""
    # Dense music catalogs (dozens of albums) need more than a couple of
    # swipes before the target leaves the viewport in either direction.
    for move in (None, *([True] * 14), *([False] * 14)):
        if move is not None: swipe(up=move)
        node = adrv.find(pattern)
        # Items clipped by the bottom edge are tappable but only just; nudge.
        if node and node["b"][3] - node["b"][1] > 150: return node
    return None


def ui_error_text():
    hits = [l for l in labels() if re.search(r"(?i)error|failed|unsupported|cannot|could not|unable", l)]
    return " | ".join(hits)[:200]


def foreground():
    return PKG in adrv.adb("shell", "dumpsys", "activity", "activities").split("topResumedActivity", 1)[-1][:200]


def launch():
    adrv.adb("shell", "monkey", "-p", PKG, "-c", "android.intent.category.LAUNCHER", "1")


def go_home():
    """Return to the Library root without ever backing out of the app."""
    adrv.adb("shell", "cmd", "statusbar", "collapse")
    for _ in range(8):
        if not foreground():
            launch(); time.sleep(4)
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


def media_title(media):
    """Visible server-media label, keeping file_name as the operation identity."""
    name = media.get('file_name') or ''
    if media.get('type') != 'audio':
        return name
    music = media.get('music')
    title = music.get('title') if isinstance(music, dict) else None
    if isinstance(title, str) and title.strip():
        return title.strip()
    return re.sub(r'\.[^.]+$', '', name) or name


def open_detail(set_name, file_name):
    if not ensure_grid(set_name): return False
    node = find_scrolling("^" + re.escape(file_name))
    if not node: return False
    adrv.tap_node(node); time.sleep(3)
    return adrv.wait(r"^(Play Video|Play Audio|View Image)$", timeout=10) is not None


def connect_and_login(base, user, password, record, shot, check_wrong_password=False):
    """Fresh app data, connect to base, sign in; True once Library shows."""
    adrv.adb("shell", "am", "force-stop", PKG)
    adrv.adb("shell", "pm", "clear", PKG)
    launch()
    ok = adrv.wait("^Connect to server$", timeout=40)
    record("app starts in local mode with a Connect to server action", ok)
    adrv.tap("^Connect to server$"); time.sleep(2)
    adrv.tap(".", cls="EditText"); time.sleep(1)
    clear_field(); adrv.type_text(base); time.sleep(1)
    adrv.tap("^Save URL$")
    signed_in_screen = adrv.wait("^Welcome back$", timeout=25) is not None
    record("saving the server URL leads to Sign In", signed_in_screen)
    if not signed_in_screen:
        shot("login-connect-error")
        return False
    fields = [n for n in adrv.nodes() if n["cls"] == "EditText"]
    deadline = time.monotonic() + 10
    while len(fields) < 2 and time.monotonic() < deadline:
        time.sleep(0.7)
        fields = [n for n in adrv.nodes() if n["cls"] == "EditText"]
    if len(fields) < 2:
        record("sign-in fields are visible", False)
        shot("login-fields-error")
        return False
    adrv.tap_node(fields[0]); time.sleep(1); adrv.type_text(user)
    if check_wrong_password:
        _reject_wrong_password(record, shot)
    else:
        adrv.tap_node(fields[1]); time.sleep(1)
    return sign_in(password)


def _reject_wrong_password(record, shot):
    """Submit a wrong password, expect a visible error, then clear the field."""
    fields = [n for n in adrv.nodes() if n["cls"] == "EditText"]
    adrv.tap_node(fields[1]); time.sleep(1); adrv.type_text("definitely-wrong-pw")
    # The IME Enter key can only dismiss the keyboard on this screen. Submit
    # through the visible control so this check actually sends the bad login.
    adrv.tap("^Sign In$", cls="Button")
    seen = ""
    for _ in range(4):
        seen = ui_error_text() or " | ".join(l for l in labels() if re.search(r"(?i)invalid|incorrect|credential", l))
        if seen: break
    shot("login-wrong-password")
    still_login = adrv.find("^Welcome back$") is not None
    record("wrong password is rejected with a visible message", bool(seen) and still_login, seen or "no error text found in UI")
    fields = [n for n in adrv.nodes() if n["cls"] == "EditText"]
    adrv.tap_node(fields[1]); time.sleep(1); clear_field(25)


def sign_in(password):
    """Type the password and submit through the visible control if needed."""
    adrv.type_text(password)
    adrv.adb("shell", "input", "keyevent", "KEYCODE_ENTER"); time.sleep(2)
    if adrv.wait("^Library$", timeout=5):
        return True
    if adrv.find("^Welcome back$"):
        # Enter may only dismiss the IME. Back can now close the whole
        # activity if that happened, so press the actual submit button.
        adrv.tap("^Sign In$", cls="Button")
    return adrv.wait("^Library$", timeout=30) is not None
