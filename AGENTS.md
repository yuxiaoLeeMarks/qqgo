# AGENTS.md

## Tests

Only write a test for behaviour that can actually break. If the code under test could change without the test noticing, or the test would fail on a harmless rewording, don't write it.

**Write tests for:**

- What input does: clicks, keys, drags, gestures and scrolling, and the state or effect they lead to (seeking when the progress bar is let go, Space after opening Now Playing, back and forward, flinging the cover, liking a song, leaving a radio).
- Logic that doesn't need a view: parsing lyrics, formatting times and counts, the player's queue, seeking, fading and cache, decoding streams and images.
- A regression you just fixed. Say in the test's comment what went wrong.
- Frame budgets, in `perf_test.go`, so idle views don't start asking for frames they don't need.

**Don't write tests that:**

- Check that a list of labels shows on screen. That repeats the view code and breaks whenever the wording changes.
- Check default settings or the order of rows in a form.
- Test MyGo itself, for example that `AnimateTo` or a spring moves from 0 to 1.
- Depend on the wall clock. `ui.NewTester` starts its clock at the current time, so anything with a phase taken from the clock passes or fails depending on when the test runs. Also, `FramePending` ignores frames waiting on `After` or `Painter.After`.

**How:**

- Build views with `ui.NewTester` and set up the app with `newTestApp` (in `internal/app/helpers_test.go`). Find elements by their accessibility label, then `Click`, `RightClick`, `Press`/`Move`/`Release` or `Key`, and check the app's state afterwards.
- Swap out the network and the clipboard through package variables such as `likeSong` and `copyText`, and restore the real one with `defer`.
- Live tests that call QQ Music skip unless `LIVE` is set. Run them (`LIVE=1 go test ./internal/qqmusic/ ./internal/player/`) after changing a request: the gateway's answers change shape without notice.
- QQ Music addresses songs, artists and albums by MID; a page's route carries the MID (`route.mid`), playlists their numeric ID. The listener's own playlists are edited by their directory (`Playlist.DirID`).
- To see the real app, run a build with `NM_SHOTS=<dir>` (and `NM_SCENE=tour` for the pages the default scene skips): it walks its views and saves each window as a PNG (`internal/app/debug_shots.go`). Point `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` at a temporary directory so it doesn't touch the listener's own session.
- Look at visuals instead of asserting on them. The `NM_FRAMES` tests save PNGs into that directory and skip when it isn't set. Check pixels only for coverage, such as whether a layer still covers the window.
- Start each test with a one-sentence comment saying the behaviour it checks, in the style of the code.
- Run `go test ./internal/...` before committing, and run a new UI test with `-count=5`. Fix or delete a flaky test; don't retry it.
