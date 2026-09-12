# Ghost Racer Telemetry Web

English · [简体中文](README.md)

Reads the lap telemetry recorded by **Ghost Racer Enhanced** (a BeamNG.drive ghost-lap mod, a separate project) and shows the racing line, the channel traces and a multi-lap Δt comparison in a browser.

One Go binary, no dependencies, listening on `127.0.0.1` by default. Two ways to get the data, usable together:

1. **Read the save folder directly** (default): the ghostReplays tree under the BeamNG user folder is scanned at start-up, and again whenever you press *Rescan* after a session. No mod change needed.
2. **Push from inside the game** (endpoint ready): an "export for analysis" button in the mod POSTs the selected recordings to `/api/import`, and the service stores them alongside the game's own libraries. The contract is in [`docs/import-api.md`](docs/import-api.md).

## Running it

Grab a single-file binary for your platform from [Releases](https://github.com/flintt/ghost-racer-telemetry/releases), or build it yourself:

```bash
go build -o ghost-racer-telemetry .      # Windows: GOOS=windows GOARCH=amd64 go build -o ghost-racer-telemetry.exe .
./ghost-racer-telemetry
```

With no arguments it looks for the BeamNG user folder and opens <http://127.0.0.1:8777/>. On Windows it tries these in order:

```text
%LOCALAPPDATA%\BeamNG\BeamNG.drive\current\      ← current installs (current is a junction to the active version)
%LOCALAPPDATA%\BeamNG\BeamNG.drive\<version>\    ← highest version number when there is no current
%LOCALAPPDATA%\BeamNG.drive\<version>\           ← older installs
```

To point it somewhere else:

```bash
./ghost-racer-telemetry -root "C:\Users\you\AppData\Local\BeamNG\BeamNG.drive\current"
```

`-root` accepts the user folder, a version folder, or the `ghostReplays` directory itself. When nothing is found it prints every path it tried.

> **Do not pass the game folder to `-data`.** `-root` is the **read-only** game save; `-data` is the service's own **writable** import directory. Getting them the wrong way round still shows the recordings, but that tree is then treated as the service's own data: deleting is no longer guarded by `-allow-delete` and really removes the recording from the game. The start-up output warns when it looks like this happened.

### Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `-addr` | `127.0.0.1:8777` | Listen address. Keep it on loopback unless you mean to expose it |
| `-root` | auto-detected | BeamNG user folder or its `ghostReplays` directory, **read only** |
| `-data` | `bng_ghost_web/ghostReplays` under the user config dir | Writable directory for laps exported from the game |
| `-web` | embedded | Serve the UI from a directory instead of the embedded copy |
| `-allow-delete` | `false` | Allow deleting laps from the **game** folder. Close BeamNG first, or it will write its in-memory manifest back |
| `-token` | empty | Require a shared secret on `/api/import` (`X-Ghost-Token` header or `?token=`) |

## The interface

- **Sidebar**: `level → saved start → track variant`, the same hierarchy as in game, collapsible per node with the open/closed state remembered in the browser. Levels start collapsed once there are more than six of them (an all-open tree of 74 libraries is unreadable); the library you have open expands itself and scrolls into view. A start with a single variant renders as a leaf rather than another level of indentation. `P2P` is point to point, `TT` is a Time Trial, `IMP` was pushed in from the game. Searching expands everything.
- **Recording list**: sort by lap time, rank or record order; filter by category (complete / incomplete / manual) and by vehicle. Ranks count only timed complete laps, matching the mod's own categories. Each row can be exported as CSV or deleted.
- **Track map**: the line from above, coloured by speed, throttle/brake, gear, lateral G, Δt or per recording. The start gate is a green dashed line, the finish gate (point to point) a yellow one.
- **Charts**: speed, Δt, throttle/brake, lateral G, longitudinal acceleration and gear. Hovering the map or any chart moves one shared cursor, and the readout above the charts shows every selected recording's values at that point.
- **Summary**: lap time, distance, top/average/minimum speed, peak acceleration and braking, peak lateral G, full-throttle/braking/coasting share, climb, sample count and vehicle.
- **Language** switches between English and Chinese (picked from the browser on first visit), and the **theme** has auto / light / dark (auto follows the system). Both choices are remembered in the browser.

### Zooming

The two panes zoom independently:

- **Track map**: the wheel zooms about the pointer (up to 40×), dragging pans once zoomed, and a double-click or the **1:1** button in the corner restores the fit. The point under the pointer stays pinned, so putting the pointer on a corner and scrolling magnifies that corner.
- **Charts**: the wheel zooms the X window (down to 1/500 of the lap), dragging pans, a double-click resets. All six charts share the window, and the cursor and readout follow it. **The Y axis is scaled to what is inside the window**, so zooming into one corner actually expands its speed trace instead of leaving it flat against the whole-lap range. A `Full · 900–1500 m` reset button appears in the bar.

**A zoomed map follows the cursor.** Moving the cursor on the charts — or hovering a row in the sector table — pans the map when the matching point approaches the edge, by the least amount that brings it back inside, so scrubbing looks like the map sliding along with the point. Only a jump of more than a screen (landing on a different sector, say) recentres outright. Over a 1200-step sweep of a whole lap at 23× zoom the point never left the viewport once. Dragging the map yourself, or hovering directly on it, never triggers the follow.

Switching between the distance and time axis clears the window, since a range in metres does not carry over to seconds. While zoomed, the map culls segments outside the viewport, so drawing gets cheaper the further you zoom in.

### Sector analysis (best stretches)

With two or more recordings selected, the *Sectors* tab cuts the shared route into 10 m cells, times every lap through each cell and gives the cell to whoever was quickest. Consecutive cells owned by one lap form a **stretch** — that lap's best piece of driving.

- **Ideal lap**: the sum of the quickest time in every cell. Its gap to the actual quickest lap is how much is left on the table with the driving already on record.
- **Sector table**: `range / quickest / sector time / lead over next / vs reference`, sorted by lead — the top row is the corner worth practising. Hovering a row parks the cursor in the middle of that stretch on the map and every chart.
- **Trace colour → Sector owner**: each lap keeps its colour where it owns the cell and goes grey elsewhere. The Δt chart carries the same ownership strip along its top edge.

Three decisions worth knowing about:

1. **Cells are measured along the reference lap's path, not along each lap's own travelled distance.** Own distance quietly penalises a wider line: after 500 m of its own travel a wide lap has not reached the reference's 500 m mark, yet that is where it gets compared. Measured on a line pushed 3 m wide, own travel came to 2306.3 m while the projection put it at 2289.7 m — exactly the reference's length, so 16.6 m of bias is what the projection removes.
2. **Ownership is decided on time spent inside a cell, not cumulative time.** Cumulative time (the Δt curve itself) carries an early advantage all the way to the flag and makes one lap look quicker everywhere; only per-cell time answers "who was quicker through *here*". The slope of the Δt curve is the same information.
3. **The flicker is filtered out.** A stretch shorter than three cells sandwiched between two stretches of one other lap is treated as noise and merged into it, and a stretch that leads the next lap by less than 0.02 s never reaches the table.

An abandoned fragment competes only in the cells it actually reached: it neither truncates the comparison nor takes part in the ideal-lap total. Recordings made with different cars all compete together, so use the vehicle filter when that matters.

**The X axis can be distance or time.** Distance is the default: comparing laps only means something when they are lined up by how far they have travelled from the start gate, which is how Δt is computed — the reference lap's time is interpolated to each sample's distance on this lap and subtracted. An abandoned run therefore simply ends where it stopped instead of skewing the comparison.

The URL hash carries the current view (`#lib=…&laps=1,3&ref=1&axis=dist&color=speed`), so a specific comparison can be bookmarked or shared.

## Where the data comes from

The service reads these files from the BeamNG user folder (read only, unless `-allow-delete` is set):

```text
ghostReplays/freeRoam/<level>/startLines.json                                   saved-start registry (name, position, finish gate, variant grouping)
ghostReplays/freeRoam/<level>/starts/<startId>/ghostracer.save.library.json     lap manifest
ghostReplays/freeRoam/<level>/starts/<startId>/ghostracer.save.ghosts/<id>.json samples
ghostReplays/races/<level>/<raceKey>/ghostracer.save.*                          Time Trial / race
ghostReplays/freeRoam/<level>/<vehicleDir>/...                                  pre-2.9.8 layout, also read
```

Samples are compact arrays with fixed columns:

```text
1=t  2..4=position xyz  5..7=forward vector  8..10=up vector  11=speed (m/s)
12=throttle  13=brake  14=gear  15=handbrake  16=clutch        ← 2.18 and later only
```

The original 1.6 object format (`{pos, dirFront, dirUp, speed}`, no timestamps) is read too, with timestamps derived from `sampleInterval`. Lateral G and longitudinal acceleration are computed by the service from position, speed and the forward vector; the mod does not record those channels.

## API

| Method | Path | Meaning |
| --- | --- | --- |
| `GET` | `/api/catalog` | The cached library catalog |
| `POST` | `/api/rescan` | Re-read the disk |
| `GET` | `/api/laps?lib=<key>` | Every lap in one library (metadata only) |
| `GET` | `/api/lap?lib=<key>&id=<id>` | One lap's full channels and summary |
| `DELETE` | `/api/lap?lib=<key>&id=<id>` | Delete a lap (subject to `-allow-delete`) |
| `POST` | `/api/import` | Accept recordings exported from the game, see [`docs/import-api.md`](docs/import-api.md) |

A `lib` key looks like `game:freeRoam/east_coast_usa/starts/s001/ghostracer.save.json`, prefixed by its source (`game` / `import`).

## Development

```bash
./tools/check.sh                                          # gofmt + vet + tests + JS syntax + UI smoke + both build targets
./tools/build.sh v0.1.0                                   # release artefacts in dist/ (4 targets + SHA256SUMS)
./tools/smoke.sh                                          # smoke test on its own: headless Chrome loads the real page and fails on any JS error
go run ./cmd/genfixture -out /tmp/gr/ghostReplays          # write a synthetic save tree
go run ./cmd/genfixture -out /tmp/gr/ghostReplays -size 6  # longer laps (~9500 samples each) for render stress tests
go run . -root /tmp/gr/ghostReplays -web ./web            # serve the UI from disk; reload instead of rebuilding
```

### Rendering

The map and the charts are split into a **static layer** and a **cursor layer**: traces, grids, axes and series are drawn into an offscreen canvas keyed by which laps are selected, the size, the colour mode and the X axis; moving the mouse only blits that bitmap and draws the crosshair and dots. Traces are decimated to screen pixels and their colours quantized into 32 steps so that a run of one colour becomes a single stroked path; a channel with more samples than the plot has pixel columns is reduced to per-column min/max, which keeps braking spikes instead of dropping them the way stride sampling would.

Measured in headless Chrome (software rasterizer, 5 laps / 45,909 samples): a cursor redraw takes **0.60 ms**, against **191 ms** for the same content without the cached layers.

## License

MIT, see [`LICENSE`](LICENSE). The BeamNG.drive mod that produces the recordings is a separate project under its own license.
