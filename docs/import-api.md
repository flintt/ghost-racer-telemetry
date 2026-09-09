# 游戏内导出协议 `/api/import`

服务端已经实现并可用；**mod 那边的"导出分析"按钮还没写**，这份文档是给之后接线用的契约。

## 请求

```http
POST http://127.0.0.1:8777/api/import
Content-Type: application/json
X-Ghost-Token: <仅当服务端带了 -token>
```

```jsonc
{
  "source": "ghost-racer-enhanced/2.20.3",   // 可选，写进每条记录的 importedFrom
  "level": "east_coast_usa",                 // 地图 id
  "startId": "s001",                         // 起点 id
  "startName": "Coast loop",                 // 可选，显示用
  "startKey": "s001",                        // 可选，赛道变体分组
  "kind": "freeRoam",                        // "freeRoam" | "timeTrial"
  "raceKey": null,                           // Time Trial 时给 raceKey
  "library": "ghostReplays/freeRoam/east_coast_usa/starts/s001/ghostracer.save.json",
  "startLine": { "level": "east_coast_usa", "position": [x, y, z], "normal": [x, y, 0], "halfWidth": 8 },
  "laps": [
    {
      "id": "3",                             // 缺省时用清单里的 nextId
      "label": "Lap 3",
      "lapTime": 92.418,                     // 未完成片段给 null
      "duration": 92.418,
      "sampleInterval": 0.02,
      "groundOffset": 0.35,
      "source": "lap",                       // "lap" | "manual" | "incomplete"
      "complete": true,
      "incompleteReason": null,
      "vehicle": "sunburst",
      "manual": false,
      "color": "orange",
      "pinned": false,
      "hasInputs": true,
      "samples": [[t, px,py,pz, fx,fy,fz, ux,uy,uz, speed, throttle, brake, gear, handbrake, clutch], ...]
    }
  ]
}
```

`library` 优先：给了就按它落盘（去掉 `ghostReplays/` 前缀），这样导入库的目录结构和游戏里完全一致；没给就用 `level` + `startId`/`raceKey` 拼。样本少于 2 个的圈会被跳过并在 `skipped` 里报回来。

单圈还可以直接把 replay envelope 原样 POST 上来（顶层带 `samples` 即可），会按当前时间戳生成 id。

## 响应

```json
{ "libraryKey": "import:freeRoam/east_coast_usa/starts/s001/ghostracer.save.json",
  "rel": "freeRoam/east_coast_usa/starts/s001/ghostracer.save.json",
  "written": ["3"], "skipped": [] }
```

同一个 `id` 重复导入是**替换**而不是追加，所以按钮可以随便点。写完服务端会自动重扫，前端点一下"重新扫描"或刷新就能看到。

打开对应视图的链接可以直接拼出来：

```text
http://127.0.0.1:8777/#lib=<urlencode(libraryKey)>&laps=3&axis=dist&color=speed
```

## mod 侧怎么接

推荐在 **UI App 里发请求**：`ui/modules/apps/ghostRacerApp/app.js` 跑在 CEF 里，`fetch` 直接可用，不需要 GE Lua 有 HTTP 能力。

1. Vehicle controller 里加一个只读命令，把选中记录的样本吐给 UI（UI 已有的桥是 `bngApi.activeObjectLua(lua, callback)`，返回值会 JSON 化传回来）：

   ```lua
   -- lua/vehicle/controller/ghostRacer.lua
   function M.exportGhostPayload(ids)
     local laps = {}
     for _, entry in ipairs(playbackState.ghosts) do
       if idSelected(ids, entry.id) then
         laps[#laps + 1] = {
           id = entry.id, label = entry.label, lapTime = entry.lapTime,
           duration = entry.duration, sampleInterval = entry.sampleInterval,
           groundOffset = entry.groundOffset, source = entry.source,
           complete = entry.complete ~= false, incompleteReason = entry.incompleteReason,
           vehicle = entry.vehicle, manual = entry.manual == true,
           color = entry.colorName, hasInputs = entry.hasInputs == true,
           samples = entry.samples
         }
       end
     end
     return {
       source = "ghost-racer-enhanced/" .. CODE_VERSION,
       level = startGateConfig.registry.level,
       startId = ..., startName = ..., startKey = ..., kind = ..., raceKey = ...,
       library = playbackState.activeLibraryFilename,
       laps = laps
     }
   end
   ```

2. app.js 里加一个"导出分析"按钮，拿到 payload 后 POST：

   ```js
   bngApi.activeObjectLua(`(function() local c=controller.getController("ghostRacer")
     return c and c.exportGhostPayload(${JSON.stringify(selectedIds)}) or nil end)()`,
     (payload) => {
       if (!payload) return
       fetch('http://127.0.0.1:8777/api/import', {
         method: 'POST',
         headers: {'Content-Type': 'application/json'},
         body: JSON.stringify(payload)
       }).then((response) => response.json())
         .then((result) => { /* Notice: 已导出 result.written.length 条 */ })
         .catch(() => { /* Notice: 分析服务没在跑 */ })
     })
   ```

服务端对 `/api/import` 开了 CORS（`Access-Control-Allow-Origin: *`）并处理 OPTIONS 预检，因为 CEF 页面有自己的 origin。监听在回环地址上，所以这不等于对外开放；要更严一点就用 `-token`。

**注意数据量**：50 Hz 下一圈 60 秒是 3000 个采样点、约 1.5 MB JSON，要过一次 Lua→UI 的桥。一次导出五条会明显卡一下。如果实测太慢，可选的做法是按 mod 现成的 20 Hz 分享采样率降采样（`shareCodec` 已经有这个逻辑），或者一次只推一条。
