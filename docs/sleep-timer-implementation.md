# Countdown sleep timer (ek3)

U01 §12 reserves `Z`: focus Sleep duration outside editable controls/dialogs.
Web options row: [Sleep: 15/30/45/60/90 min] [Start timer] [remaining 29:59] [Cancel].
This secondary row sits below transport and wraps on narrow screens, using theme
colors/native controls. Detached playback has the same row; its source owns expiry.
Android: a labeled Sleep timer action opens a native duration sheet; a secondary
status row shows remaining time and Cancel. Back dismisses without changing it.

Off → choose duration → Start → Running → Cancel → Off; choosing Start again replaces
the countdown. Expiry → Paused, retaining position; explicit Play resumes. Timer
continues across track changes/background and local/Cast ownership; delayed source
loads must not undo expiry. Empty/image playback disables starting. No network or
permission is needed; offline/local audio is supported. Timer is session-only:
reload/app process restart clears it, and changing the wall clock does not extend it.
Background browser scheduling can delay expiry until the page runs again; Android
background/actual output and browser suspension behavior await integrated checks.

Deferred checks: Chrome/Firefox pointer and Z/native-select typing/focus, detached
handoff, pending loads/Cast, light/dark/narrow layouts; Android sheet/Back/Cancel,
background expiry/output, restart and wall-clock changes. No runtime acceptance yet.

Implementation checks: Node sleep-timer, playback-speed (including detach), Cast,
playback-source and detach-repeat tests passed. Scoped Flutter analyze of seven
changed/test files clean; `flutter test --no-pub test/services/sleep_timer_test.dart`
passed (two tests on the final narrow rerun). No full suites, builds, browser or emulator journeys run.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Two
expert rounds were used. The initial native/new-intent and late Cast LOAD
blockers were fixed. The final review found a no-op Previous boundary that
invalidated natural advancement; its small fix was self-reviewed and verified
with an actual handler regression that advances and plays the second track.
Affected analysis and the filtered handler test pass. No third review was run.
Integrated with main-branch gain (`fc36a42`) and saved queues (`e94f09a`),
retaining both control groups and popup state fields. Focused merged checks
pass: ten Node modules, eight-file native analysis, fourteen timer/gain/queue
tests, the actual Previous-boundary handler test and two delayed-write queue
recovery cases. All 76 service-worker assets exist. Full gates/builds and actual
platform acceptance remain deferred until backlog implementation is complete.
Task ek3 is pending.
