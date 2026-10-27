# Music regression gate after task 20

The next broad regression gate is due after task 30. Between gates, verify each
implemented feature on Chrome, Firefox, and native Android with its focused
checks. Keep at most two Android emulators running; this gate used one.

## Results

| Check | Result |
| --- | --- |
| Chrome music end to end suite | 83 passed initially, 12 failed, 26 fixture specific skips; all 12 failures passed focused reruns |
| Firefox music end to end suite | 90 passed initially, 4 failed, 27 fixture specific skips; all 4 failures passed focused reruns |
| Web unit tests | Passed after fixes |
| Go `go test ./... -race -cover` | Passed after fixes |
| Flutter analysis and widget tests | Passed; 1,752 widget tests |
| Android 14 emulator keyboard integration | 4 passed |
| Android 14 emulator audio visuals integration | 1 passed; all five visual modes produced native output |
| Installed Android app and live server | Signed in, found Night Transit, played First Light, and advanced to Long Road Home |

The initial browser failures exposed a queue start position bug, catalog focus
and scroll restoration issues, an asynchronous Home/catalog action ownership
issue, and stale test assumptions around cached `blob:` sources, administrator
playlist access, and asynchronous completion. The fixes were retested in the
affected browser scenarios. Evidence and full logs are in
`/home/paul/tmp/codex/player-regression-evidence`.

The standard browser fixture skips scenarios needing separate augmentation or
external services. M39 and M42 had already passed focused Chrome, Firefox,
and Android acceptance before this gate. M21 remote receiver handoff remains
unverified because no compatible receiver was identified. This gate did not
run every specialized browser fixture or every Android integration driver.
Standalone `tsc --noEmit` still reports existing Playwright type configuration
errors (missing DOM library and unresolved browser module imports); the executed
Playwright suites passed after focused reruns.
