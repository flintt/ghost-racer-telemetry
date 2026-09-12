/*
 * Ghost Racer telemetry browser.
 *
 * Reads the catalog served by the Go backend, then draws the selected laps as
 * a track map and a stack of channel charts sharing one cursor. Laps are
 * compared on distance from the start gate, which is how the mod itself lines
 * ghosts up, so Δt is meaningful even when the laps have different durations.
 */

// One palette per theme: the dark set's white and pale yellow vanish on a light
// background, so each slot has a light-theme counterpart at the same index.
const LAP_PALETTES = {
  dark: ['#ff8b3d', '#4dd6ff', '#3ddc97', '#e56bff', '#f5f5f5', '#ffd23d', '#7aa2ff', '#ff5c72'],
  light: ['#c2570a', '#0b7fa8', '#12855b', '#9b1fb0', '#334155', '#9a6b00', '#3d4fd1', '#c0243c']
}
const MAX_SELECTED = 8

const state = {
  catalog: null,
  libraries: [],
  activeLibrary: null,
  laps: [],
  selected: [],          // {key, id, color, lap} — lap holds the loaded channels
  referenceKey: null,    // `${libKey}#${id}` of the delta reference
  filters: { category: 'all', vehicle: '', sort: 'lapTime', search: '' },
  axis: 'dist',
  colorMode: 'speed',
  cursorX: null,
  // A pinned cursor survives the mouse wandering off to the other pane; hover
  // cannot move or clear it, only another click or Esc can.
  cursorPinned: false,
  hoverLapKey: null,
  charts: [],
  collapsed: new Map(),
  lang: 'en',
  theme: 'auto',
  summaryTab: 'metrics',
  // Expanded/collapsed per panel, remembered across reloads.
  panels: { laps: true, summary: true },
  sectors: null,
  // Zoom state: an X window over the charts, a scale+pan over the map. Both are
  // view-only; nothing downstream of them recomputes lap data.
  xRange: null,
  mapView: { scale: 1, panX: 0, panY: 0 },
  mapDrag: null,
  chartDrag: null,
  // A selected stretch, in the units of the current axis, plus the in-progress
  // gesture that is drawing it.
  selection: null,
  selecting: null,
  // 'north' keeps the world upright; 'heading' turns the map so the direction of
  // travel points up, the way a phone navigates.
  mapOrientation: 'north',
  mapTilt: false,
  showRoads: false,
  roads: null,
  mapHeading: 0,
  // `engaged` outlives `playing`: pausing should freeze the laps where they were,
  // not collapse them back onto one point.
  playback: { playing: false, engaged: false, speed: 1, loop: true, time: 0, elapsed: 0, handle: null }
}

/* ------------------------------------------------------------------ i18n */

const I18N = {
  zh: {
    appTitle: 'Ghost Racer 遥测',
    searchPlaceholder: '搜索地图 / 起点 / 赛道',
    rescan: '重新扫描',
    themeAuto: '自动', themeLight: '浅色', themeDark: '深色',
    libraries: '记录库',
    pickStart: '选择一个起点',
    catAll: '全部', catLap: '完整圈', catIncomplete: '未完成', catManual: '手动录制',
    axisLabel: 'X 轴', axisDistance: '距离', axisTime: '时间',
    colorLabel: '轨迹着色',
    mapEmpty: '勾选左侧记录以查看轨迹',
    rootGame: '游戏存档', rootImport: '导入', rootMissing: '未找到',
    rootSummary: (count, path) => `${count} 个库 · ${path}`,
    noRoots: '没有配置任何数据目录',
    libraryCount: (count) => `${count} 个`,
    treeNoMatch: '没有匹配的记录库',
    treeEmpty: '没有找到记录，确认 -root 指向 BeamNG 用户目录',
    levelDetail: (libraries, laps) => `${libraries} 库 · ${laps} 圈`,
    variantDetail: (count) => `${count} 变体`,
    searchExpandsAll: '搜索时展开全部',
    libraryTip: (rel, total, complete, incomplete, manual) =>
      `${rel}\n${total} 圈（完整 ${complete} · 未完成 ${incomplete} · 手动 ${manual}）`,
    lapsCount: (count) => `${count} 圈`,
    tagImport: '导入',
    pointToPoint: '点对点',
    allVehicles: '全部车型',
    sortLapTime: '按圈速', sortRank: '按名次', sortId: '按记录顺序', sortDuration: '按时长',
    colorSpeed: '速度', colorInputs: '油门/刹车', colorGear: '档位',
    colorLatG: '横向 G', colorDelta: 'Δt 对比', colorPerLap: '按记录配色', colorSector: '分段归属',
    tabMetrics: '汇总', tabSectors: '分段',
    resetZoom: '1:1', resetRange: '全程 ·',
    collapse: '折叠', expand: '展开',
    pinned: '已锁定', pinRelease: '释放（Esc）', pinCentre: '把地图移到锁定点',
    pinHint: '点击锁定游标 · 锁定后移到另一侧不会丢位置',
    selectHint: '右键或 Ctrl 拖动框选一段',
    orientHeading: '切换为行进方向朝上', orientNorth: '切换为正北朝上',
    tiltOn: '倾斜视角（3D）', tiltOff: '取消倾斜，回到俯视',
    roadsOn: '显示赛道路面（从游戏关卡文件读取）', roadsOff: '隐藏赛道路面',
    roadsUnavailable: '读不到赛道数据：',
    play: '播放所选区间', pause: '暂停', loop: '循环',
    selectionNone: '未选区间（播放整圈）',
    zoomHint: '滚轮缩放 · 拖动平移 · 双击还原',
    idealLap: (ideal, gap, coverage) =>
      `理论最佳 ${ideal} · 比最快圈快 ${gap} · 覆盖 ${coverage} m`,
    sectorsNeedTwo: '至少选两条记录才能做分段对比',
    sectorsNone: '没有足够显著的分段差异',
    sectorRange: '区间 (m)', sectorOwner: '最快',
    sectorRate: '速率 (s/100m)', sectorPeak: '峰值 (s/100m)',
    sectorGain: '累计领先 (s)', sectorVsRef: '相对参照圈 (s)',
    sectorPlay: '选中这一段并播放',
    lapListEmptyFiltered: '当前筛选下没有记录',
    lapListEmpty: '这个库里还没有记录',
    exportCsvTitle: '导出 CSV', deleteTitle: '删除这条记录',
    deleteConfirm: (label) => `删除记录「${label}」？如果 BeamNG 正在运行，它可能会把这条记录再写回来。`,
    deleted: '已删除', rescanned: '已重新扫描',
    maxSelected: (max) => `最多同时对比 ${max} 条记录`,
    incompleteLabel: (reason) => `未完成${reason ? ' · ' + reason : ''}`,
    manualLabel: '手动', noInputs: '无输入',
    chipReferenceTitle: '点击设为 Δt 参照圈', chipRemoveTitle: '移除',
    metric: '指标',
    sumLapTime: '圈速', sumDuration: '时长 (s)', sumDistance: '距离 (m)',
    sumTopSpeed: '最高速 (km/h)', sumAvgSpeed: '平均速 (km/h)', sumMinSpeed: '最低速 (km/h)',
    sumMaxAccel: '最大加速 (m/s²)', sumMaxDecel: '最大减速 (m/s²)', sumMaxLatG: '最大横向 G',
    sumFullThrottle: '全油门占比', sumBraking: '刹车占比', sumCoasting: '滑行占比',
    sumElevation: '爬升 (m)', sumSamples: '采样点', sumVehicle: '车型',
    chartSpeed: '速度 (km/h)', chartDelta: 'Δt vs 参照圈 (s)', chartInputs: '油门 / 刹车 (%)',
    chartLatG: '横向 G', chartAccel: '纵向加速度 (m/s²)', chartGear: '档位',
    chartElevation: '海拔 (m)', chartGradient: '坡度 (%)',
    colorElevation: '海拔', colorGradient: '坡度',
    sumMaxClimb: '最陡上坡', sumMaxDescent: '最陡下坡',
    legendDown: '下坡', legendUp: '上坡',
    legendGearLow: '低档', legendGearHigh: '高档',
    legendDeltaGain: '追回时间', legendDeltaLoss: '丢失时间',
    legendBrake: '刹车', legendThrottle: '全油门',
    readoutHint: '把鼠标移到轨迹或曲线上查看该点数据；点击可锁定位置',
    readoutSummary: (count, reference) => `${count} 条记录 · 参照圈 ${reference}`,
    readoutPosition: '位置', readoutTime: '时间', readoutEnded: '已结束',
    readoutThrottle: '油', readoutBrake: '刹', readoutGear: 'G'
  },
  en: {
    appTitle: 'Ghost Racer Telemetry',
    searchPlaceholder: 'Search level / start / track',
    rescan: 'Rescan',
    themeAuto: 'Auto', themeLight: 'Light', themeDark: 'Dark',
    libraries: 'Libraries',
    pickStart: 'Pick a start',
    catAll: 'All', catLap: 'Laps', catIncomplete: 'Incomplete', catManual: 'Manual',
    axisLabel: 'X axis', axisDistance: 'Distance', axisTime: 'Time',
    colorLabel: 'Trace colour',
    mapEmpty: 'Tick a recording on the left to draw its line',
    rootGame: 'Game saves', rootImport: 'Imports', rootMissing: 'not found',
    rootSummary: (count, path) => `${count} libraries · ${path}`,
    noRoots: 'No data directory configured',
    libraryCount: (count) => `${count}`,
    treeNoMatch: 'No library matches',
    treeEmpty: 'No recordings found — check that -root points at the BeamNG user folder',
    levelDetail: (libraries, laps) => `${libraries} lib · ${laps} laps`,
    variantDetail: (count) => `${count} variants`,
    searchExpandsAll: 'Everything is expanded while searching',
    libraryTip: (rel, total, complete, incomplete, manual) =>
      `${rel}\n${total} laps (complete ${complete} · incomplete ${incomplete} · manual ${manual})`,
    lapsCount: (count) => `${count} laps`,
    tagImport: 'IMP',
    pointToPoint: 'point to point',
    allVehicles: 'All vehicles',
    sortLapTime: 'By lap time', sortRank: 'By rank', sortId: 'By record order', sortDuration: 'By duration',
    colorSpeed: 'Speed', colorInputs: 'Throttle/brake', colorGear: 'Gear',
    colorLatG: 'Lateral G', colorDelta: 'Δt vs reference', colorPerLap: 'Per recording',
    colorSector: 'Sector owner',
    tabMetrics: 'Summary', tabSectors: 'Sectors',
    resetZoom: '1:1', resetRange: 'Full ·',
    collapse: 'Collapse', expand: 'Expand',
    pinned: 'Pinned', pinRelease: 'Release (Esc)', pinCentre: 'Bring the map to the pinned point',
    pinHint: 'Click to pin the cursor · a pinned position survives moving to the other pane',
    selectHint: 'Right-drag or Ctrl-drag to select a stretch',
    orientHeading: 'Turn the map heading-up', orientNorth: 'Turn the map north-up',
    tiltOn: 'Tilt the view (3D)', tiltOff: 'Drop the tilt, look straight down',
    roadsOn: 'Show the road surface, read from the game level files',
    roadsOff: 'Hide the road surface',
    roadsUnavailable: 'Road data unavailable:',
    play: 'Play the selected stretch', pause: 'Pause', loop: 'Loop',
    selectionNone: 'No selection (plays the whole lap)',
    zoomHint: 'Wheel to zoom · drag to pan · double-click to reset',
    idealLap: (ideal, gap, coverage) =>
      `Ideal lap ${ideal} · ${gap} under the quickest · over ${coverage} m`,
    sectorsNeedTwo: 'Pick at least two recordings to compare sectors',
    sectorsNone: 'No sector difference worth reporting',
    sectorRange: 'Range (m)', sectorOwner: 'Quickest',
    sectorRate: 'Rate (s/100 m)', sectorPeak: 'Peak (s/100 m)',
    sectorGain: 'Total lead (s)', sectorVsRef: 'Vs reference (s)',
    sectorPlay: 'Select this stretch and play it',
    lapListEmptyFiltered: 'No recording matches this filter',
    lapListEmpty: 'This library has no recordings yet',
    exportCsvTitle: 'Export CSV', deleteTitle: 'Delete this recording',
    deleteConfirm: (label) => `Delete "${label}"? If BeamNG is running it may write this recording back.`,
    deleted: 'Deleted', rescanned: 'Rescanned',
    maxSelected: (max) => `At most ${max} recordings can be compared at once`,
    incompleteLabel: (reason) => `incomplete${reason ? ' · ' + reason : ''}`,
    manualLabel: 'manual', noInputs: 'no inputs',
    chipReferenceTitle: 'Click to use as the Δt reference', chipRemoveTitle: 'Remove',
    metric: 'Metric',
    sumLapTime: 'Lap time', sumDuration: 'Duration (s)', sumDistance: 'Distance (m)',
    sumTopSpeed: 'Top speed (km/h)', sumAvgSpeed: 'Average speed (km/h)', sumMinSpeed: 'Min speed (km/h)',
    sumMaxAccel: 'Max accel (m/s²)', sumMaxDecel: 'Max decel (m/s²)', sumMaxLatG: 'Max lateral G',
    sumFullThrottle: 'Full throttle', sumBraking: 'Braking', sumCoasting: 'Coasting',
    sumElevation: 'Climb (m)', sumSamples: 'Samples', sumVehicle: 'Vehicle',
    chartSpeed: 'Speed (km/h)', chartDelta: 'Δt vs reference (s)', chartInputs: 'Throttle / brake (%)',
    chartLatG: 'Lateral G', chartAccel: 'Longitudinal accel (m/s²)', chartGear: 'Gear',
    chartElevation: 'Elevation (m)', chartGradient: 'Gradient (%)',
    colorElevation: 'Elevation', colorGradient: 'Gradient',
    sumMaxClimb: 'Steepest climb', sumMaxDescent: 'Steepest descent',
    legendDown: 'downhill', legendUp: 'uphill',
    legendGearLow: 'low gear', legendGearHigh: 'high gear',
    legendDeltaGain: 'gaining', legendDeltaLoss: 'losing',
    legendBrake: 'brake', legendThrottle: 'full throttle',
    readoutHint: 'Hover the map or a chart to read that point; click to pin it',
    readoutSummary: (count, reference) => `${count} recordings · reference ${reference}`,
    readoutPosition: 'At', readoutTime: 'Time', readoutEnded: 'ended',
    readoutThrottle: 'thr', readoutBrake: 'brk', readoutGear: 'G'
  }
}

const LANG_STORAGE = 'ghostRacerWeb.lang'
const THEME_STORAGE = 'ghostRacerWeb.theme'
const PANEL_STORAGE = 'ghostRacerWeb.panels'
const ORIENTATION_STORAGE = 'ghostRacerWeb.mapOrientation'
const TILT_STORAGE = 'ghostRacerWeb.mapTilt'
const ROADS_STORAGE = 'ghostRacerWeb.showRoads'

function t(key, ...args) {
  const table = I18N[state.lang] || I18N.en
  const value = table[key] !== undefined ? table[key] : I18N.en[key]
  return typeof value === 'function' ? value(...args) : value
}

function readSetting(key, fallback) {
  try {
    return localStorage.getItem(key) || fallback
  } catch (error) {
    return fallback
  }
}

function writeSetting(key, value) {
  try {
    localStorage.setItem(key, value)
  } catch (error) {
    // Private windows refuse storage; the choice just will not persist.
  }
}

function detectLanguage() {
  const stored = readSetting(LANG_STORAGE, '')
  if (stored === 'zh' || stored === 'en') return stored
  return (navigator.language || 'en').toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

// applyStaticText fills every element carrying a data-i18n hook, and rebuilds
// the selects, whose options are built in script.
function applyStaticText() {
  document.documentElement.lang = state.lang === 'zh' ? 'zh-CN' : 'en'
  document.title = t('appTitle')
  for (const node of document.querySelectorAll('[data-i18n]')) {
    node.textContent = t(node.dataset.i18n)
  }
  for (const node of document.querySelectorAll('[data-i18n-placeholder]')) {
    node.placeholder = t(node.dataset.i18nPlaceholder)
  }
  el('map').title = `${t('zoomHint')}\n${t('pinHint')}\n${t('selectHint')}`
  updateZoomControls()
  applyPanels()
  fillSelect(el('sortMode'), [
    ['lapTime', t('sortLapTime')], ['rank', t('sortRank')],
    ['id', t('sortId')], ['duration', t('sortDuration')]
  ], state.filters.sort)
  fillSelect(el('colorMode'), [
    ['speed', t('colorSpeed')], ['throttle', t('colorInputs')], ['gear', t('colorGear')],
    ['latg', t('colorLatG')], ['elevation', t('colorElevation')], ['gradient', t('colorGradient')],
    ['delta', t('colorDelta')], ['sector', t('colorSector')], ['lap', t('colorPerLap')]
  ], state.colorMode)
  for (const node of el('langMode').children) {
    node.classList.toggle('active', node.dataset.lang === state.lang)
  }
  for (const node of el('themeMode').children) {
    node.classList.toggle('active', node.dataset.themeMode === state.theme)
  }
}

function fillSelect(select, options, selected) {
  select.textContent = ''
  for (const [value, label] of options) select.append(new Option(label, value))
  select.value = selected
}

function setLanguage(lang) {
  if (state.lang === lang) return
  state.lang = lang
  writeSetting(LANG_STORAGE, lang)
  applyStaticText()
  renderRootStatus()
  renderTree()
  if (state.activeLibrary) describeLibrary(state.activeLibrary)
  render()
}

/* ---------------------------------------------------------------- panels */

function loadPanels() {
  try {
    const stored = JSON.parse(localStorage.getItem(PANEL_STORAGE) || '{}')
    return {
      laps: stored.laps !== false,
      summary: stored.summary !== false
    }
  } catch (error) {
    return { laps: true, summary: true }
  }
}

// applyPanels reflects the collapse state into the layout. The canvases size
// themselves from their containers, so a redraw has to follow the reflow.
function applyPanels() {
  const content = document.querySelector('.content')
  content.classList.toggle('laps-collapsed', !state.panels.laps)
  el('lapPanel').classList.toggle('collapsed', !state.panels.laps)
  el('summaryTable').hidden = !state.panels.summary

  const lapToggle = el('lapPanelToggle')
  lapToggle.textContent = state.panels.laps ? '▾' : '▸'
  lapToggle.setAttribute('aria-expanded', String(state.panels.laps))
  lapToggle.title = state.panels.laps ? t('collapse') : t('expand')

  const summaryToggle = el('summaryToggle')
  summaryToggle.textContent = state.panels.summary ? '▾' : '▸'
  summaryToggle.setAttribute('aria-expanded', String(state.panels.summary))
  summaryToggle.title = state.panels.summary ? t('collapse') : t('expand')

  // Expanding has to refill the table: rendering is skipped while it is hidden.
  renderSummary()
  scheduleRedraw()
}

function togglePanel(name) {
  state.panels[name] = !state.panels[name]
  writeSetting(PANEL_STORAGE, JSON.stringify(state.panels))
  applyPanels()
}

/* ----------------------------------------------------------------- theme */

// Canvas has no cascade, so the chart colours are read out of the same CSS
// custom properties the rest of the UI uses, and re-read when the theme flips.
let themeColorCache = null

function themeColor(name) {
  if (!themeColorCache) {
    themeColorCache = {}
    const styles = getComputedStyle(document.documentElement)
    for (const key of ['--chart-grid', '--chart-text', '--chart-zero', '--chart-cursor',
      '--chart-cursor-pinned', '--chart-dot-ring', '--chart-reference-dim', '--trace-muted',
      '--overlay', '--line', '--accent', '--road-fill', '--road-edge']) {
      themeColorCache[key] = styles.getPropertyValue(key).trim()
    }
  }
  return themeColorCache[name]
}

function effectiveTheme() {
  if (state.theme !== 'auto') return state.theme
  return window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'
}

function applyTheme() {
  if (state.theme === 'auto') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.dataset.theme = state.theme
  themeColorCache = null
  assignLapColors()
  for (const node of el('themeMode').children) {
    node.classList.toggle('active', node.dataset.themeMode === state.theme)
  }
}

function setTheme(theme) {
  state.theme = theme
  writeSetting(THEME_STORAGE, theme)
  applyTheme()
  render()
}

const el = (id) => document.getElementById(id)
const lapKey = (entry) => `${entry.key}#${entry.id}`

/* ---------------------------------------------------------------- utilities */

function formatTime(seconds) {
  if (seconds == null || !isFinite(seconds)) return '—'
  const sign = seconds < 0 ? '-' : ''
  const value = Math.abs(seconds)
  const minutes = Math.floor(value / 60)
  const rest = value - minutes * 60
  return `${sign}${minutes}:${rest.toFixed(3).padStart(6, '0')}`
}

function formatDelta(seconds) {
  if (seconds == null || !isFinite(seconds)) return '—'
  return `${seconds >= 0 ? '+' : ''}${seconds.toFixed(3)}`
}

function kmh(metersPerSecond) { return metersPerSecond * 3.6 }

function toast(message, isError) {
  const node = el('toast')
  node.textContent = message
  node.classList.toggle('error', Boolean(isError))
  node.hidden = false
  clearTimeout(toast.timer)
  toast.timer = setTimeout(() => { node.hidden = true }, isError ? 6000 : 2600)
}

async function api(path, options) {
  const response = await fetch(path, options)
  const body = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(body.error || `${response.status} ${response.statusText}`)
  return body
}

/* ------------------------------------------------------------ library tree */

async function loadCatalog(rescan) {
  const catalog = await api(rescan ? '/api/rescan' : '/api/catalog', rescan ? { method: 'POST' } : undefined)
  state.catalog = catalog
  state.libraries = catalog.libraries || []
  renderRootStatus()
  renderTree()
  if (state.activeLibrary) {
    const same = state.libraries.find((library) => library.key === state.activeLibrary.key)
    if (same) await openLibrary(same, true)
  }
}

function renderRootStatus() {
  const roots = state.catalog.roots || []
  const parts = roots.map((root) => {
    const label = root.name === 'game' ? t('rootGame') : t('rootImport')
    if (!root.exists) return `${label}: ${t('rootMissing')}`
    return `${label}: ${t('rootSummary', root.count, root.path)}`
  })
  el('rootStatus').textContent = parts.join('　|　') || t('noRoots')
  el('libraryCount').textContent = t('libraryCount', state.libraries.length)
}

// The sidebar mirrors the in-game hierarchy: level → saved start → track
// variant. Groups collapse, and the open/closed state survives a reload — with
// dozens of libraries a flat list is unusable.
const COLLAPSE_STORAGE = 'ghostRacerWeb.tree'
// Levels start open for a handful of maps and closed once there are many, where
// an all-open tree is just a wall of names. An explicit click always wins and is
// remembered per node.
const AUTO_COLLAPSE_LEVELS = 6

function loadTreeState() {
  try {
    return new Map(JSON.parse(localStorage.getItem(COLLAPSE_STORAGE) || '[]'))
  } catch (error) {
    return new Map()
  }
}

function saveTreeState() {
  try {
    localStorage.setItem(COLLAPSE_STORAGE, JSON.stringify([...state.collapsed]))
  } catch (error) {
    // A private window can refuse storage; the tree still works for this session.
  }
}

function nodeOpen(nodeId, fallback) {
  return state.collapsed.has(nodeId) ? state.collapsed.get(nodeId) : fallback
}

function toggleCollapsed(nodeId, current) {
  state.collapsed.set(nodeId, !current)
  saveTreeState()
  renderTree()
}

// buildTree groups the flat library list and applies the search filter. A group
// holding a single library is flattened into a leaf so one-track starts do not
// cost an extra level of indentation.
function buildTree() {
  const search = state.filters.search.toLowerCase()
  const levels = new Map()

  for (const library of state.libraries) {
    const haystack = `${library.level} ${library.startName} ${library.startId} ${library.raceKey}`.toLowerCase()
    if (search && !haystack.includes(search)) continue
    if (!levels.has(library.level)) levels.set(library.level, new Map())
    const groups = levels.get(library.level)
    const groupKey = library.startKey || library.startId || library.rel
    if (!groups.has(groupKey)) groups.set(groupKey, [])
    groups.get(groupKey).push(library)
  }

  const model = []
  for (const [level, groups] of [...levels].sort((a, b) => a[0].localeCompare(b[0]))) {
    const node = { level, groups: [], libraryCount: 0, lapCount: 0 }
    for (const [groupKey, libraries] of [...groups].sort((a, b) => a[0].localeCompare(b[0]))) {
      libraries.sort((a, b) => a.startName.localeCompare(b.startName))
      node.groups.push({
        key: `${level}/${groupKey}`,
        name: libraries[0].startName || groupKey,
        libraries
      })
      node.libraryCount += libraries.length
      node.lapCount += libraries.reduce((total, library) => total + library.lapCount, 0)
    }
    node.groups.sort((a, b) => a.name.localeCompare(b.name))
    model.push(node)
  }
  return model
}

function renderTree() {
  const tree = el('tree')
  tree.textContent = ''
  const model = buildTree()
  // While searching, everything matching is shown open — hunting for a start
  // and then having to expand it defeats the search.
  const searching = state.filters.search.length > 0
  const activeKey = state.activeLibrary ? state.activeLibrary.key : null
  const levelsOpenByDefault = model.length <= AUTO_COLLAPSE_LEVELS

  if (model.length === 0) {
    const empty = document.createElement('p')
    empty.className = 'muted'
    empty.style.padding = '12px 8px'
    empty.textContent = state.libraries.length ? t('treeNoMatch') : t('treeEmpty')
    tree.append(empty)
    return
  }

  for (const level of model) {
    const levelId = `level:${level.level}`
    const holdsActive = level.groups.some((group) => group.libraries.some((library) => library.key === activeKey))
    const open = searching || holdsActive || nodeOpen(levelId, levelsOpenByDefault)
    const levelNode = document.createElement('div')
    levelNode.className = 'tree-node level'
    levelNode.append(branchHeader({
      id: levelId,
      label: level.level,
      detail: t('levelDetail', level.libraryCount, level.lapCount),
      open,
      locked: searching
    }))

    if (open) {
      const children = document.createElement('div')
      children.className = 'tree-children'
      for (const group of level.groups) {
        if (group.libraries.length === 1) {
          children.append(libraryLeaf(group.libraries[0]))
          continue
        }
        const groupId = `group:${group.key}`
        const groupOpen = searching ||
          group.libraries.some((library) => library.key === activeKey) ||
          nodeOpen(groupId, true)
        const groupNode = document.createElement('div')
        groupNode.className = 'tree-node group'
        groupNode.append(branchHeader({
          id: groupId,
          label: group.name,
          detail: t('variantDetail', group.libraries.length),
          open: groupOpen,
          locked: searching
        }))
        if (groupOpen) {
          const variants = document.createElement('div')
          variants.className = 'tree-children'
          for (const library of group.libraries) variants.append(libraryLeaf(library, true))
          groupNode.append(variants)
        }
        children.append(groupNode)
      }
      levelNode.append(children)
    }
    tree.append(levelNode)
  }

  // Keep the open library visible when the tree is long.
  const active = tree.querySelector('.tree-item.active')
  if (active && active.scrollIntoView) active.scrollIntoView({ block: 'nearest' })
}

function branchHeader({ id, label, detail, open, locked }) {
  const header = document.createElement('button')
  header.className = 'tree-branch'
  header.setAttribute('aria-expanded', String(open))

  const caret = document.createElement('span')
  caret.className = 'caret'
  caret.textContent = open ? '▾' : '▸'
  const name = document.createElement('span')
  name.className = 'name'
  name.textContent = label
  name.title = label
  const count = document.createElement('span')
  count.className = 'count'
  count.textContent = detail
  header.append(caret, name, count)

  if (locked) {
    header.disabled = true
    header.title = t('searchExpandsAll')
  } else {
    header.addEventListener('click', () => toggleCollapsed(id, open))
  }
  return header
}

function libraryLeaf(library, nested) {
  const item = document.createElement('div')
  item.className = nested ? 'tree-item nested' : 'tree-item'
  if (state.activeLibrary && state.activeLibrary.key === library.key) item.classList.add('active')

  const name = document.createElement('span')
  name.className = 'name'
  name.textContent = library.startName || library.startId
  name.title = t('libraryTip', library.rel, library.lapCount,
    library.completeCount, library.incompleteCount, library.manualCount)
  item.append(name)

  if (library.pointToPoint) item.append(tag('P2P', 'p2p'))
  if (library.kind === 'timeTrial' || library.kind === 'race') item.append(tag('TT', 'tt'))
  if (library.root === 'import') item.append(tag(t('tagImport'), 'import'))

  const count = document.createElement('span')
  count.className = 'count'
  count.textContent = library.bestLapTime ? formatTime(library.bestLapTime) : t('lapsCount', library.lapCount)
  item.append(count)

  item.addEventListener('click', () => openLibrary(library))
  return item
}

function tag(text, kind) {
  const node = document.createElement('span')
  node.className = `tag ${kind}`
  node.textContent = text
  return node
}

/* --------------------------------------------------------------- lap list */

async function openLibrary(library, keepSelection) {
  state.activeLibrary = library
  renderTree()
  describeLibrary(library)

  try {
    const payload = await api(`/api/laps?lib=${encodeURIComponent(library.key)}`)
    state.laps = payload.laps || []
  } catch (error) {
    state.laps = []
    toast(error.message, true)
  }

  const vehicles = [...new Set(state.laps.map((lap) => lap.vehicle).filter(Boolean))].sort()
  const filter = el('vehicleFilter')
  const previous = filter.value
  filter.textContent = ''
  filter.append(new Option(t('allVehicles'), ''))
  for (const vehicle of vehicles) filter.append(new Option(vehicle, vehicle))
  filter.value = vehicles.includes(previous) ? previous : ''
  state.filters.vehicle = filter.value

  if (!keepSelection) {
    state.selected = []
    state.referenceKey = null
    render()
  }
  renderLapList()
  if (!keepSelection) {
    // Opening a library is nearly always "show me the best lap here".
    const best = visibleLaps().find((lap) => lap.rank === 1) || visibleLaps()[0]
    if (best) await toggleLap(best)
  }
}

function describeLibrary(library) {
  el('libraryTitle').textContent = library.startName || library.startId
  const bits = [library.level, library.rel]
  if (library.pointToPoint) bits.push(t('pointToPoint'))
  if (library.bestLapTime) bits.push(`PB ${formatTime(library.bestLapTime)}`)
  el('libraryMeta').textContent = bits.join(' · ')
}

function visibleLaps() {
  const { category, vehicle, sort } = state.filters
  const laps = state.laps.filter((lap) => {
    if (category !== 'all' && lap.category !== category) return false
    if (vehicle && lap.vehicle !== vehicle) return false
    return true
  })
  const sorters = {
    lapTime: (a, b) => (a.lapTime ?? Infinity) - (b.lapTime ?? Infinity),
    rank: (a, b) => (a.rank || 99) - (b.rank || 99),
    id: (a, b) => Number(a.id) - Number(b.id),
    duration: (a, b) => (b.duration || 0) - (a.duration || 0)
  }
  return laps.sort(sorters[sort] || sorters.lapTime)
}

function renderLapList() {
  const list = el('lapList')
  list.textContent = ''
  const reference = referenceLap()
  const laps = visibleLaps()
  if (laps.length === 0) {
    const row = list.insertRow()
    const cell = row.insertCell()
    cell.colSpan = 6
    cell.className = 'muted'
    cell.textContent = state.laps.length ? t('lapListEmptyFiltered') : t('lapListEmpty')
    return
  }

  for (const lap of laps) {
    const key = `${state.activeLibrary.key}#${lap.id}`
    const chosen = state.selected.find((entry) => lapKey(entry) === key)
    const row = list.insertRow()
    row.className = chosen ? 'selected' : ''

    const pick = row.insertCell()
    pick.className = 'col-pick'
    const box = document.createElement('input')
    box.type = 'checkbox'
    box.checked = Boolean(chosen)
    box.addEventListener('change', () => toggleLap(lap))
    pick.append(box)

    const rank = row.insertCell()
    rank.innerHTML = lap.rank
      ? `<span class="rank ${lap.rank === 1 ? 'p1' : ''}">#${lap.rank}</span>`
      : '<span class="rank">—</span>'

    const label = row.insertCell()
    label.className = 'col-label'
    const swatch = document.createElement('span')
    swatch.className = 'swatch'
    swatch.style.background = chosen ? chosen.color : 'var(--line)'
    label.append(swatch, document.createTextNode(lapDescription(lap)))
    label.title = lapDescription(lap)

    const time = row.insertCell()
    time.className = 'col-time'
    time.textContent = lap.lapTime ? formatTime(lap.lapTime) : `${(lap.duration || 0).toFixed(1)}s`

    const delta = row.insertCell()
    delta.className = 'delta-cell'
    if (reference && reference.lapTime && lap.lapTime && lapKey(reference) !== key) {
      const difference = lap.lapTime - reference.lapTime
      delta.textContent = formatDelta(difference)
      delta.classList.add(difference >= 0 ? 'pos' : 'neg')
    }

    const actions = row.insertCell()
    actions.className = 'row-actions'
    actions.append(iconButton('CSV', t('exportCsvTitle'), () => exportCsv(lap)))
    actions.append(iconButton('✕', t('deleteTitle'), () => deleteLap(lap), true))

    row.addEventListener('click', (event) => {
      if (event.target.tagName === 'INPUT' || event.target.tagName === 'BUTTON') return
      toggleLap(lap)
    })
  }
}

function lapDescription(lap) {
  const bits = [lap.label || `Lap ${lap.id}`]
  if (lap.vehicle) bits.push(lap.vehicle)
  if (lap.category === 'incomplete') bits.push(t('incompleteLabel', lap.incompleteReason))
  if (lap.category === 'manual') bits.push(t('manualLabel'))
  if (lap.pinned) bits.push('📌')
  if (!lap.hasInputs) bits.push(t('noInputs'))
  return bits.join(' · ')
}

function iconButton(text, title, handler, danger) {
  const button = document.createElement('button')
  button.textContent = text
  button.title = title
  if (danger) button.className = 'danger'
  button.addEventListener('click', (event) => { event.stopPropagation(); handler() })
  return button
}

/* ------------------------------------------------------------- selection */

function referenceLap() {
  if (!state.selected.length) return null
  return state.selected.find((entry) => lapKey(entry) === state.referenceKey) || state.selected[0]
}

async function toggleLap(lapMeta) {
  const key = `${state.activeLibrary.key}#${lapMeta.id}`
  const existing = state.selected.findIndex((entry) => lapKey(entry) === key)
  if (existing >= 0) {
    state.selected.splice(existing, 1)
    if (state.referenceKey === key) state.referenceKey = state.selected.length ? lapKey(state.selected[0]) : null
    render()
    return
  }
  if (state.selected.length >= MAX_SELECTED) {
    toast(t('maxSelected', MAX_SELECTED), true)
    return
  }

  const colorIndex = nextColorIndex()
  const entry = {
    key: state.activeLibrary.key,
    id: lapMeta.id,
    colorIndex,
    color: lapPalette()[colorIndex],
    lap: null
  }
  state.selected.push(entry)
  if (!state.referenceKey) state.referenceKey = lapKey(entry)
  renderLapList()
  try {
    entry.lap = await api(`/api/lap?lib=${encodeURIComponent(entry.key)}&id=${encodeURIComponent(entry.id)}`)
  } catch (error) {
    state.selected = state.selected.filter((item) => item !== entry)
    toast(error.message, true)
  }
  render()
}

function lapPalette() {
  return LAP_PALETTES[effectiveTheme()] || LAP_PALETTES.dark
}

function nextColorIndex() {
  const used = new Set(state.selected.map((entry) => entry.colorIndex))
  for (let index = 0; index < LAP_PALETTES.dark.length; index += 1) {
    if (!used.has(index)) return index
  }
  return state.selected.length % LAP_PALETTES.dark.length
}

// assignLapColors re-resolves every selected lap's colour after a theme change;
// the slot (colorIndex) is what is stable, not the hex value.
function assignLapColors() {
  const colors = lapPalette()
  for (const entry of state.selected) entry.color = colors[entry.colorIndex % colors.length]
}

function renderChips() {
  const container = el('selectedChips')
  container.textContent = ''
  const reference = referenceLap()
  for (const entry of state.selected) {
    const chip = document.createElement('span')
    chip.className = 'lap-chip'
    if (reference && lapKey(reference) === lapKey(entry)) chip.classList.add('reference')
    chip.title = t('chipReferenceTitle')

    const swatch = document.createElement('span')
    swatch.className = 'swatch'
    swatch.style.background = entry.color
    chip.append(swatch)

    const label = entry.lap ? (entry.lap.label || `Lap ${entry.id}`) : `Lap ${entry.id} …`
    const text = document.createElement('span')
    text.textContent = entry.lap && entry.lap.lapTime ? `${label} ${formatTime(entry.lap.lapTime)}` : label
    chip.append(text)

    if (reference && lapKey(reference) === lapKey(entry)) {
      const mark = document.createElement('span')
      mark.className = 'ref-mark'
      mark.textContent = 'REF'
      chip.append(mark)
    }

    const drop = document.createElement('span')
    drop.className = 'drop'
    drop.textContent = '✕'
    drop.title = t('chipRemoveTitle')
    drop.addEventListener('click', (event) => {
      event.stopPropagation()
      state.selected = state.selected.filter((item) => lapKey(item) !== lapKey(entry))
      if (state.referenceKey === lapKey(entry)) {
        state.referenceKey = state.selected.length ? lapKey(state.selected[0]) : null
      }
      render()
    })
    chip.append(drop)

    chip.addEventListener('click', () => {
      state.referenceKey = lapKey(entry)
      render()
    })
    container.append(chip)
  }
}

/* ----------------------------------------------------------------- deltas */

// interpolate returns series[i] at the given x, walking the (monotonic) axis.
function interpolate(axisValues, series, x) {
  const total = axisValues.length
  if (total === 0) return null
  if (x <= axisValues[0]) return series[0]
  if (x >= axisValues[total - 1]) return series[total - 1]
  let low = 0
  let high = total - 1
  while (high - low > 1) {
    const middle = (low + high) >> 1
    if (axisValues[middle] <= x) low = middle
    else high = middle
  }
  const span = axisValues[high] - axisValues[low]
  if (span <= 0) return series[low]
  const ratio = (x - axisValues[low]) / span
  return series[low] + (series[high] - series[low]) * ratio
}

function indexAt(axisValues, x) {
  const total = axisValues.length
  if (total === 0) return -1
  if (x <= axisValues[0]) return 0
  if (x >= axisValues[total - 1]) return total - 1
  let low = 0
  let high = total - 1
  while (high - low > 1) {
    const middle = (low + high) >> 1
    if (axisValues[middle] <= x) low = middle
    else high = middle
  }
  return x - axisValues[low] <= axisValues[high] - x ? low : high
}

// computeDelta builds the cumulative time difference against the reference lap,
// sampled at this lap's own points. Laps are aligned on distance travelled from
// the start gate, so a lap that stops short simply ends its trace early.
function computeDelta(entry, reference) {
  if (!entry.lap || !reference || !reference.lap || entry === reference) return null
  const own = entry.lap.channels
  const other = reference.lap.channels
  if (!own.dist.length || !other.dist.length) return null
  const delta = new Array(own.dist.length)
  for (let i = 0; i < own.dist.length; i += 1) {
    const referenceTime = interpolate(other.dist, other.t, own.dist[i])
    delta[i] = own.t[i] - referenceTime
  }
  entry.deltaAgainst = lapKey(reference)
  return delta
}

function refreshDeltas() {
  const reference = referenceLap()
  for (const entry of state.selected) {
    entry.delta = computeDelta(entry, reference)
  }
}

/* --------------------------------------------------------------- sectors */

/*
 * Micro-sector analysis: cut the shared route into fixed-length cells, time each
 * lap through every cell, and give the cell to whoever was quickest. The runs of
 * cells a lap owns are its best stretches, and summing the winning cell times
 * gives the ideal lap.
 *
 * Cells are measured along the REFERENCE lap's path, not along each lap's own
 * travelled distance. Own distance quietly penalises a wider line: after 500 m
 * of its own travel a wide lap has not yet reached the reference's 500 m mark,
 * so comparing there compares two different places on the track.
 */

const SECTOR_STEP = 10        // metres per cell
const SECTOR_MIN_CELLS = 3    // a shorter run is noise, not a stretch
const SECTOR_MIN_GAIN = 0.02  // seconds; below this a win is not worth reporting
// Seconds per 100 m. A stretch is ranked by how FAST the gap opens, not by how
// much it added up to: total gain grows with length, so a long mild advantage
// would otherwise outrank the short corner where the difference was actually
// made. This floor drops stretches that are merely long.
const SECTOR_MIN_RATE = 0.01

// stationAt refines a nearest sample to the nearest point on the two adjacent
// path segments, so the station is continuous instead of quantised to samples.
function stationAt(reference, index, px, py) {
  let best = reference.dist[index]
  let bestDistance = Infinity
  for (const start of [index - 1, index]) {
    if (start < 0 || start + 1 >= reference.x.length) continue
    const ax = reference.x[start]
    const ay = reference.y[start]
    const vx = reference.x[start + 1] - ax
    const vy = reference.y[start + 1] - ay
    const lengthSq = vx * vx + vy * vy
    if (lengthSq === 0) continue
    let ratio = ((px - ax) * vx + (py - ay) * vy) / lengthSq
    ratio = ratio < 0 ? 0 : ratio > 1 ? 1 : ratio
    const dx = ax + vx * ratio - px
    const dy = ay + vy * ratio - py
    const distance = dx * dx + dy * dy
    if (distance < bestDistance) {
      bestDistance = distance
      best = reference.dist[start] + (reference.dist[start + 1] - reference.dist[start]) * ratio
    }
  }
  return best
}

// stationsFor projects every sample of one lap onto the reference path and
// returns how far along that path each sample sits.
function stationsFor(entry, reference) {
  const own = entry.lap.channels
  const path = reference.lap.channels
  const total = own.x.length
  const pathTotal = path.x.length
  const stations = new Float64Array(total)
  let cursor = 0

  for (let i = 0; i < total; i += 1) {
    const px = own.x[i]
    const py = own.y[i]
    let bestIndex = cursor
    let bestDistance = Infinity
    let from = Math.max(0, cursor - 4)
    let to = Math.min(pathTotal - 1, cursor + 64)
    for (;;) {
      for (let k = from; k <= to; k += 1) {
        const dx = path.x[k] - px
        const dy = path.y[k] - py
        const distance = dx * dx + dy * dy
        if (distance < bestDistance) {
          bestDistance = distance
          bestIndex = k
        }
      }
      // Widen while the best sits on the forward edge: a lap much slower than
      // the reference walks the window forward faster than it advances.
      if (bestIndex < to || to >= pathTotal - 1) break
      from = to + 1
      to = Math.min(pathTotal - 1, to + 256)
    }
    stations[i] = stationAt(path, bestIndex, px, py)
    cursor = bestIndex
  }

  // A car that stops, spins or reverses would otherwise walk the station back.
  for (let i = 1; i < total; i += 1) {
    if (stations[i] < stations[i - 1]) stations[i] = stations[i - 1]
  }
  return stations
}

function ensureStations(entries, reference) {
  const referenceKey = lapKey(reference)
  for (const entry of entries) {
    if (entry.stationsRef === referenceKey && entry.stations) continue
    entry.stations = entry === reference
      ? Float64Array.from(entry.lap.channels.dist)
      : stationsFor(entry, reference)
    entry.stationsRef = referenceKey
  }
}

// collectRuns groups the cell ownership into maximal stretches and removes the
// flicker: a stretch too short to be real, sandwiched between two stretches of
// one other lap, belongs to that lap.
function collectRuns(owner) {
  const runs = []
  let start = 0
  for (let k = 1; k <= owner.length; k += 1) {
    if (k === owner.length || owner[k] !== owner[start]) {
      runs.push({ owner: owner[start], from: start, to: k })
      start = k
    }
  }
  for (let i = 1; i < runs.length - 1; i += 1) {
    const run = runs[i]
    if (run.to - run.from >= SECTOR_MIN_CELLS) continue
    if (runs[i - 1].owner !== runs[i + 1].owner) continue
    for (let k = run.from; k < run.to; k += 1) owner[k] = runs[i - 1].owner
  }
  const merged = []
  start = 0
  for (let k = 1; k <= owner.length; k += 1) {
    if (k === owner.length || owner[k] !== owner[start]) {
      merged.push({ owner: owner[start], from: start, to: k })
      start = k
    }
  }
  return merged
}

function computeSectors() {
  state.sectors = null
  const entries = loadedEntries()
  const reference = referenceLap()
  if (!reference || !reference.lap || entries.length < 2) return

  ensureStations(entries, reference)

  // The grid spans the furthest lap, not the shortest: an abandoned fragment
  // should add information where it ran, not truncate the whole comparison.
  const reach = entries.map((entry) => entry.stations[entry.stations.length - 1])
  const cells = Math.floor(Math.max(...reach) / SECTOR_STEP)
  if (cells < 4) return

  // Time at every cell boundary, then the time spent inside each cell. Cell
  // times are what decides ownership: cumulative time would carry an early
  // advantage all the way to the flag.
  const cellTimes = entries.map((entry) => {
    const times = entry.lap.channels.t
    const nodes = new Float64Array(cells + 1)
    for (let k = 0; k <= cells; k += 1) {
      nodes[k] = interpolate(entry.stations, times, k * SECTOR_STEP)
    }
    const spent = new Float64Array(cells)
    for (let k = 0; k < cells; k += 1) spent[k] = nodes[k + 1] - nodes[k]
    entry.sectorTotal = nodes[cells] - nodes[0]
    entry.sectorReach = entry.stations[entry.stations.length - 1]
    return spent
  })

  const owner = new Int16Array(cells).fill(-1)
  let ideal = 0
  for (let k = 0; k < cells; k += 1) {
    let bestIndex = -1
    let bestTime = Infinity
    for (let i = 0; i < entries.length; i += 1) {
      // A lap that ended earlier cannot win a cell it never reached.
      if (reach[i] < (k + 1) * SECTOR_STEP) continue
      const spent = cellTimes[i][k]
      if (spent > 0 && spent < bestTime) {
        bestTime = spent
        bestIndex = i
      }
    }
    owner[k] = bestIndex
    if (bestIndex >= 0) ideal += bestTime
  }

  const referenceIndex = entries.indexOf(reference)
  const runs = []
  for (const run of collectRuns(owner)) {
    if (run.owner < 0 || run.to - run.from < SECTOR_MIN_CELLS) continue
    let gain = 0
    let versusReference = 0
    let ownerTime = 0
    let peakCellGain = 0
    for (let k = run.from; k < run.to; k += 1) {
      const winner = cellTimes[run.owner][k]
      ownerTime += winner
      if (referenceIndex >= 0) versusReference += cellTimes[referenceIndex][k] - winner
      // "Worth" of a stretch is how much it beat the next quickest lap by.
      let runnerUp = Infinity
      for (let i = 0; i < entries.length; i += 1) {
        if (i === run.owner || reach[i] < (k + 1) * SECTOR_STEP) continue
        const spent = cellTimes[i][k]
        if (spent > 0 && spent < runnerUp) runnerUp = spent
      }
      if (isFinite(runnerUp)) {
        const cellGain = runnerUp - winner
        gain += cellGain
        if (cellGain > peakCellGain) peakCellGain = cellGain
      }
    }
    const length = (run.to - run.from) * SECTOR_STEP
    // Rates are seconds per 100 m: how quickly the gap opens, which is what
    // makes one stretch better driven than another. The peak is the steepest
    // single cell, so a short burst inside a long mild stretch still shows.
    const rate = (gain / length) * 100
    const peakRate = (peakCellGain / SECTOR_STEP) * 100
    if (gain < SECTOR_MIN_GAIN || rate < SECTOR_MIN_RATE) continue
    runs.push({
      entry: entries[run.owner],
      from: run.from * SECTOR_STEP,
      to: run.to * SECTOR_STEP,
      length,
      time: ownerTime,
      gain,
      rate,
      peakRate,
      versusReference
    })
  }
  // Ranked by rate: the stretch where the gap opens fastest comes first.
  runs.sort((a, b) => b.rate - a.rate)

  // Only a lap that covered the whole grid has a total worth comparing with the
  // ideal; a fragment's "total" is just the time of the part it ran.
  const complete = entries.filter((entry) => entry.sectorReach >= cells * SECTOR_STEP)
  const fastest = complete.reduce((best, entry) =>
    (best === null || entry.sectorTotal < best.sectorTotal ? entry : best), null)

  state.sectors = {
    step: SECTOR_STEP,
    cells,
    owner,
    runs,
    ideal,
    coverage: cells * SECTOR_STEP,
    fastest,
    fastestTime: fastest ? fastest.sectorTotal : null,
    index: new Map(entries.map((entry, i) => [lapKey(entry), i])),
    signature: `${lapKey(reference)}|${selectionSignature()}`
  }
}

// sectorOwnsSample answers, for the track map, whether this lap owns the cell a
// given sample falls in.
function sectorOwnsSample(entry, index) {
  const sectors = state.sectors
  if (!sectors || !entry.stations) return false
  const cell = Math.floor(entry.stations[index] / sectors.step)
  if (cell < 0 || cell >= sectors.cells) return false
  return sectors.owner[cell] === sectors.index.get(lapKey(entry))
}

/* --------------------------------------------------------------- summary */

const SUMMARY_ROWS = [
  { key: 'sumLapTime', get: (lap) => (lap.lapTime ? formatTime(lap.lapTime) : '—') },
  { key: 'sumDuration', get: (lap) => lap.summary.duration.toFixed(2) },
  { key: 'sumDistance', get: (lap) => lap.summary.distance.toFixed(0) },
  { key: 'sumTopSpeed', get: (lap) => kmh(lap.summary.topSpeed).toFixed(1) },
  { key: 'sumAvgSpeed', get: (lap) => kmh(lap.summary.avgSpeed).toFixed(1) },
  { key: 'sumMinSpeed', get: (lap) => kmh(lap.summary.minSpeed).toFixed(1) },
  { key: 'sumMaxAccel', get: (lap) => lap.summary.maxAccel.toFixed(2) },
  { key: 'sumMaxDecel', get: (lap) => lap.summary.maxDecel.toFixed(2) },
  { key: 'sumMaxLatG', get: (lap) => lap.summary.maxLatG.toFixed(2) },
  { key: 'sumFullThrottle', get: (lap) => (lap.hasInputs ? `${lap.summary.fullThrottlePct.toFixed(1)}%` : '—') },
  { key: 'sumBraking', get: (lap) => (lap.hasInputs ? `${lap.summary.brakingPct.toFixed(1)}%` : '—') },
  { key: 'sumCoasting', get: (lap) => (lap.hasInputs ? `${lap.summary.coastingPct.toFixed(1)}%` : '—') },
  { key: 'sumElevation', get: (lap) => lap.summary.elevationGain.toFixed(0) },
  { key: 'sumMaxClimb', get: (lap) => `${(lap.summary.maxGradient || 0).toFixed(1)}%` },
  { key: 'sumMaxDescent', get: (lap) => `${(lap.summary.minGradient || 0).toFixed(1)}%` },
  { key: 'sumSamples', get: (lap) => String(lap.summary.sampleCount) },
  { key: 'sumVehicle', get: (lap) => lap.vehicle || '—' }
]

function renderSummary() {
  const container = el('summaryTable')
  container.textContent = ''
  renderIdealLap()
  for (const node of el('summaryTab').children) {
    node.classList.toggle('active', node.dataset.tab === state.summaryTab)
  }
  if (!state.panels.summary) return
  if (state.summaryTab === 'sectors') {
    renderSectorTable(container)
    return
  }

  const loaded = state.selected.filter((entry) => entry.lap)
  if (!loaded.length) return

  const table = document.createElement('table')
  const head = table.createTHead().insertRow()
  head.insertCell().outerHTML = `<th>${t('metric')}</th>`
  for (const entry of loaded) {
    const cell = document.createElement('th')
    const swatch = document.createElement('span')
    swatch.className = 'swatch'
    swatch.style.background = entry.color
    cell.append(swatch, document.createTextNode(entry.lap.label || `Lap ${entry.id}`))
    head.append(cell)
  }

  const body = table.createTBody()
  for (const definition of SUMMARY_ROWS) {
    const row = body.insertRow()
    row.insertCell().textContent = t(definition.key)
    for (const entry of loaded) row.insertCell().textContent = definition.get(entry.lap)
  }
  container.append(table)
}

function renderIdealLap() {
  const line = el('idealLap')
  const sectors = state.sectors
  if (!sectors || sectors.fastestTime == null) {
    line.textContent = ''
    return
  }
  line.textContent = t('idealLap',
    formatTime(sectors.ideal),
    formatDelta(sectors.ideal - sectors.fastestTime),
    sectors.coverage.toFixed(0))
}

// The sector table answers "which corner should I go and practise": every
// stretch a lap owned, worst gap first.
function renderSectorTable(container) {
  const sectors = state.sectors
  if (!sectors) {
    const hint = document.createElement('p')
    hint.className = 'muted'
    hint.style.padding = '10px 12px'
    hint.textContent = t('sectorsNeedTwo')
    container.append(hint)
    return
  }
  if (!sectors.runs.length) {
    const hint = document.createElement('p')
    hint.className = 'muted'
    hint.style.padding = '10px 12px'
    hint.textContent = t('sectorsNone')
    container.append(hint)
    return
  }

  const table = document.createElement('table')
  table.className = 'sector-table'
  const head = table.createTHead().insertRow()
  for (const key of ['sectorRange', 'sectorOwner', 'sectorRate', 'sectorPeak', 'sectorGain', 'sectorVsRef']) {
    const cell = document.createElement('th')
    cell.textContent = t(key)
    head.append(cell)
  }
  head.append(document.createElement('th'))

  const body = table.createTBody()
  const reference = referenceLap()
  for (const run of sectors.runs) {
    const row = body.insertRow()
    const range = row.insertCell()
    range.textContent = `${run.from.toFixed(0)} – ${run.to.toFixed(0)}`
    range.title = `${run.length.toFixed(0)} m · ${run.time.toFixed(3)} s`

    const owner = row.insertCell()
    const wrap = document.createElement('span')
    wrap.className = 'sector-owner'
    const swatch = document.createElement('span')
    swatch.className = 'swatch'
    swatch.style.background = run.entry.color
    wrap.append(swatch, document.createTextNode(run.entry.lap.label || run.entry.id))
    owner.append(wrap)

    const rate = row.insertCell()
    rate.textContent = run.rate.toFixed(3)
    rate.className = 'gain'
    row.insertCell().textContent = run.peakRate.toFixed(3)
    const gain = row.insertCell()
    gain.textContent = run.gain.toFixed(3)
    const versus = row.insertCell()
    versus.textContent = reference && lapKey(reference) === lapKey(run.entry)
      ? '—'
      : formatDelta(-run.versusReference)
    versus.className = run.versusReference >= 0 ? 'neg' : 'pos'

    // Hovering a row parks the cursor in that stretch on every chart and the
    // map. Stretches are measured in metres, so on the time axis the middle of
    // the stretch has to be turned into the reference lap's time there.
    const stretchValue = () => {
      const middle = (run.from + run.to) / 2
      if (state.axis === 'dist') return middle
      const anchor = reference && reference.stations ? reference : run.entry
      if (!anchor || !anchor.stations) return null
      return interpolate(anchor.stations, anchor.lap.channels.t, middle)
    }
    const actions = row.insertCell()
    actions.className = 'row-actions'
    actions.append(iconButton('▶', t('sectorPlay'), () => selectSectorRun(run, true)))

    if (state.selectedRunKey === `${run.entry.id}:${run.from}:${run.to}`) {
      row.classList.add('selected')
    }
    row.addEventListener('mouseenter', () => {
      const value = stretchValue()
      if (value != null) setCursor(value, { source: 'table' })
    })
    // Clicking a stretch makes it the selection, so it highlights on the map and
    // the transport can replay exactly that piece.
    row.addEventListener('click', (event) => {
      if (event.target.tagName === 'BUTTON') return
      selectSectorRun(run, false)
    })
  }
  container.append(table)
}

/* ------------------------------------------------------- export / delete */

function exportCsv(lapMeta) {
  const entry = state.selected.find((item) => lapKey(item) === `${state.activeLibrary.key}#${lapMeta.id}`)
  const load = entry && entry.lap
    ? Promise.resolve(entry.lap)
    : api(`/api/lap?lib=${encodeURIComponent(state.activeLibrary.key)}&id=${encodeURIComponent(lapMeta.id)}`)

  load.then((lap) => {
    const channels = lap.channels
    const columns = ['t', 'dist', 'x', 'y', 'z', 'speed', 'accel', 'latG', 'heading']
    if (lap.hasInputs) columns.push('throttle', 'brake', 'gear', 'handbrake', 'clutch')
    const lines = [columns.join(',')]
    for (let i = 0; i < channels.t.length; i += 1) {
      lines.push(columns.map((column) => channels[column][i]).join(','))
    }
    const blob = new Blob([lines.join('\n')], { type: 'text/csv' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = `${lap.level}-${lap.startName || 'start'}-lap${lap.id}.csv`.replace(/[^\w.\-]+/g, '_')
    link.click()
    setTimeout(() => URL.revokeObjectURL(link.href), 1000)
  }).catch((error) => toast(error.message, true))
}

async function deleteLap(lapMeta) {
  if (!confirm(t('deleteConfirm', lapMeta.label || lapMeta.id))) return
  try {
    await api(`/api/lap?lib=${encodeURIComponent(state.activeLibrary.key)}&id=${encodeURIComponent(lapMeta.id)}`,
      { method: 'DELETE' })
    state.selected = state.selected.filter((entry) => lapKey(entry) !== `${state.activeLibrary.key}#${lapMeta.id}`)
    toast(t('deleted'))
    await loadCatalog(true)
    await openLibrary(state.activeLibrary, true)
    render()
  } catch (error) {
    toast(error.message, true)
  }
}

/* ------------------------------------------------------------- rendering */

/*
 * Drawing is split into a static layer and a cursor overlay. The traces, grids
 * and labels are painted once into an offscreen canvas keyed by everything that
 * can change them; moving the mouse only blits that bitmap and draws the
 * crosshair. Without this, one mousemove redrew ~100k points across seven
 * canvases.
 */

const SPEED_RAMP = ['#2b6cff', '#22c1c3', '#3ddc97', '#ffd23d', '#ff8b3d', '#ff4d4d']
const DIVERGING = ['#4dd6ff', '#2b6cff', '#4a5568', '#ff8b3d', '#ff4d4d']
const THROTTLE_RAMP = ['#1f7a4d', '#3ddc97']
const BRAKE_RAMP = ['#ff8b3d', '#ff2d2d']
const COAST_COLOR = '#5a6675'
const PALETTE_STEPS = 32

const paletteCache = new Map()

// palette quantizes a gradient into fixed steps. Colours then compare as small
// integers, so a trace can be batched into one path per step instead of one
// stroke per sample.
function palette(ramp) {
  const cacheKey = ramp.join('')
  let colors = paletteCache.get(cacheKey)
  if (colors) return colors
  colors = []
  for (let step = 0; step < PALETTE_STEPS; step += 1) {
    colors.push(mixRamp(ramp, step / (PALETTE_STEPS - 1)))
  }
  paletteCache.set(cacheKey, colors)
  return colors
}

function bucket(ratio) {
  const clamped = ratio <= 0 ? 0 : ratio >= 1 ? 1 : ratio
  return Math.round(clamped * (PALETTE_STEPS - 1))
}

function rampColor(ramp, ratio) { return palette(ramp)[bucket(ratio)] }

function mixRamp(ramp, ratio) {
  const scaled = Math.max(0, Math.min(1, ratio)) * (ramp.length - 1)
  const index = Math.min(ramp.length - 2, Math.floor(scaled))
  return mixHex(ramp[index], ramp[index + 1], scaled - index)
}

function mixHex(from, to, ratio) {
  const parse = (hex) => [1, 3, 5].map((offset) => parseInt(hex.slice(offset, offset + 2), 16))
  const [r1, g1, b1] = parse(from)
  const [r2, g2, b2] = parse(to)
  const blend = (a, b) => Math.round(a + (b - a) * ratio)
  return `rgb(${blend(r1, r2)},${blend(g1, g2)},${blend(b1, b2)})`
}

function axisValues(lap) {
  return state.axis === 'dist' ? lap.channels.dist : lap.channels.t
}

// axisWindow is the X range the charts currently show: the full extent unless
// the reader has zoomed in.
function axisWindow() {
  const full = axisMax()
  if (!state.xRange) return { from: 0, to: full, full }
  const from = Math.max(0, Math.min(state.xRange.from, full))
  const to = Math.min(full, Math.max(state.xRange.to, from + full / 1000))
  return { from, to, full }
}

function isZoomed() {
  const window = axisWindow()
  return window.from > 0 || window.to < window.full
}

function axisMax() {
  let max = 0
  for (const entry of state.selected) {
    if (!entry.lap) continue
    const values = axisValues(entry.lap)
    if (values.length) max = Math.max(max, values[values.length - 1])
  }
  return max || 1
}

function loadedEntries() {
  return state.selected.filter((entry) => entry.lap && entry.lap.channels.t.length)
}

// selectionSignature captures which laps are drawn, in which colours. The
// reference lap is added only by the layers that actually depend on it (the map
// and the Δt chart), so switching REF does not repaint every channel.
function selectionSignature() {
  return state.selected
    .map((entry) => `${lapKey(entry)}:${entry.color}:${entry.lap ? entry.lap.summary.sampleCount : 0}`)
    .join('|')
}

// fitCanvas matches the backing store to the element's rendered size.
//
// The charts get an explicit height from their definition. The map must NOT:
// writing an inline height onto it makes the card's height depend on the canvas
// while the canvas reads its height from the card, and the pair locks at
// whatever they measured first — collapsing a panel or resizing the window then
// moves nothing. Its box comes from CSS (height: 100%) and is only measured here.
function fitCanvas(canvas, cssHeight) {
  const ratio = window.devicePixelRatio || 1
  let width
  let height
  if (cssHeight) {
    canvas.style.height = `${cssHeight}px`
    width = canvas.parentElement.clientWidth
    height = cssHeight
  } else {
    const rect = canvas.getBoundingClientRect()
    width = Math.max(1, Math.round(rect.width))
    height = Math.max(1, Math.round(rect.height))
  }
  const pixelWidth = Math.max(1, Math.round(width * ratio))
  const pixelHeight = Math.max(1, Math.round(height * ratio))
  if (canvas.width !== pixelWidth || canvas.height !== pixelHeight) {
    canvas.width = pixelWidth
    canvas.height = pixelHeight
  }
  const context = canvas.getContext('2d')
  context.setTransform(ratio, 0, 0, ratio, 0, 0)
  return { context, width, height, ratio }
}

// ensureLayer repaints the offscreen buffer only when its key or size changed.
function ensureLayer(holder, width, height, ratio, key, paint) {
  if (holder.key === key && holder.width === width && holder.height === height && holder.ratio === ratio) {
    return holder.canvas
  }
  const buffer = holder.canvas || document.createElement('canvas')
  buffer.width = Math.max(1, Math.round(width * ratio))
  buffer.height = Math.max(1, Math.round(height * ratio))
  const context = buffer.getContext('2d')
  context.setTransform(ratio, 0, 0, ratio, 0, 0)
  context.clearRect(0, 0, width, height)
  // Size first: the paint callback culls against the holder's dimensions.
  Object.assign(holder, { canvas: buffer, width, height, ratio })
  paint(context)
  holder.key = key
  return buffer
}

/* ----------------------------------------------------------------- roads */

// ensureRoads pulls the road geometry around the laps being shown. The service
// reads it out of the game's own level archive, so the width at every node is
// the game's, and the edges drawn from it are the real ones.
async function ensureRoads() {
  if (!state.showRoads || !state.activeLibrary) return
  const entries = loadedEntries()
  if (!entries.length) return

  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const entry of entries) {
    const [x0, y0, x1, y1] = entry.lap.summary.bounds
    minX = Math.min(minX, x0); minY = Math.min(minY, y0)
    maxX = Math.max(maxX, x1); maxY = Math.max(maxY, y1)
  }
  const level = state.activeLibrary.level
  const key = [level, minX.toFixed(0), minY.toFixed(0), maxX.toFixed(0), maxY.toFixed(0)].join(':')
  if (state.roads && state.roads.key === key) return

  const query = `level=${encodeURIComponent(level)}&minx=${minX.toFixed(1)}&miny=${minY.toFixed(1)}` +
    `&maxx=${maxX.toFixed(1)}&maxy=${maxY.toFixed(1)}&margin=300`
  try {
    const payload = await api(`/api/roads?${query}`)
    state.roads = { key, level, roads: payload.roads || [] }
  } catch (error) {
    // A missing game install is not a failure of the page; say so once and stop
    // asking for this view.
    state.roads = { key, level, roads: [], error: error.message }
    toast(`${t('roadsUnavailable')} ${error.message}`, true)
  }
  scheduleRedraw()
}

// paintRoads fills each road between its edges, which are the centre line offset
// by half the width recorded at every node.
function paintRoads(context, projection) {
  if (!state.showRoads || !state.roads || !state.roads.roads.length) return
  const fill = themeColor('--road-fill')
  const edge = themeColor('--road-edge')

  for (const road of state.roads.roads) {
    const nodes = road.nodes
    if (!nodes || nodes.length < 2) continue
    const left = []
    const right = []
    for (let i = 0; i < nodes.length; i += 1) {
      const previous = nodes[Math.max(0, i - 1)]
      const next = nodes[Math.min(nodes.length - 1, i + 1)]
      const dx = next[0] - previous[0]
      const dy = next[1] - previous[1]
      const length = Math.hypot(dx, dy) || 1
      const half = nodes[i][3] / 2
      const nx = (-dy / length) * half
      const ny = (dx / length) * half
      left.push(projection.project(nodes[i][0] + nx, nodes[i][1] + ny, nodes[i][2]))
      right.push(projection.project(nodes[i][0] - nx, nodes[i][1] - ny, nodes[i][2]))
    }

    context.beginPath()
    context.moveTo(left[0][0], left[0][1])
    for (let i = 1; i < left.length; i += 1) context.lineTo(left[i][0], left[i][1])
    for (let i = right.length - 1; i >= 0; i -= 1) context.lineTo(right[i][0], right[i][1])
    context.closePath()
    context.fillStyle = fill
    context.fill()
    context.strokeStyle = edge
    context.lineWidth = 1
    context.stroke()
  }
}

/* ------------------------------------------------------------- track map */

const mapLayer = { canvas: null, key: '', width: 0, height: 0, ratio: 0 }

function colorScales() {
  let speedMin = Infinity
  let speedMax = -Infinity
  let latgMax = 0.1
  let deltaRate = 0.02
  let elevationMin = Infinity
  let elevationMax = -Infinity
  let gradientMax = 1
  for (const entry of loadedEntries()) {
    elevationMin = Math.min(elevationMin, entry.lap.summary.minZ)
    elevationMax = Math.max(elevationMax, entry.lap.summary.maxZ)
    gradientMax = Math.max(gradientMax,
      Math.abs(entry.lap.summary.maxGradient || 0), Math.abs(entry.lap.summary.minGradient || 0))
    speedMin = Math.min(speedMin, entry.lap.summary.minSpeed)
    speedMax = Math.max(speedMax, entry.lap.summary.topSpeed)
    latgMax = Math.max(latgMax, entry.lap.summary.maxLatG)
    if (entry.delta) {
      for (let i = 25; i < entry.delta.length; i += 5) {
        deltaRate = Math.max(deltaRate, Math.abs(entry.delta[i] - entry.delta[i - 25]))
      }
    }
  }
  if (!isFinite(speedMin)) { speedMin = 0; speedMax = 1 }
  if (!isFinite(elevationMin)) { elevationMin = 0; elevationMax = 1 }
  return {
    speedMin, speedMax, speedSpan: speedMax - speedMin, latgMax, deltaRate,
    elevationMin, elevationSpan: Math.max(1, elevationMax - elevationMin), gradientMax
  }
}

// pointColorKey returns a small integer identifying a sample's colour, so runs
// of equal colour are found by integer comparison. colorForKey turns it back
// into a CSS colour once per run.
function pointColorKey(entry, index, scales) {
  const channels = entry.lap.channels
  switch (state.colorMode) {
    case 'speed':
      return bucket((channels.speed[index] - scales.speedMin) / (scales.speedSpan || 1))
    case 'throttle':
      if (!entry.lap.hasInputs) return -1
      if (channels.brake[index] > 0.05) return 100 + bucket(channels.brake[index])
      if (channels.throttle[index] > 0.05) return 200 + bucket(channels.throttle[index])
      return 300
    case 'gear':
      if (!entry.lap.hasInputs) return -1
      return bucket(Math.max(0, Math.min(8, channels.gear[index])) / 8)
    case 'latg':
      return bucket(0.5 + channels.latG[index] / (2 * (scales.latgMax || 1)))
    case 'elevation':
      return bucket((channels.z[index] - scales.elevationMin) / scales.elevationSpan)
    case 'gradient':
      if (!channels.gradient) return -1
      return bucket(0.5 + channels.gradient[index] / (2 * scales.gradientMax))
    case 'delta': {
      if (!entry.delta) return -1
      const rate = entry.delta[index] - (entry.delta[Math.max(0, index - 25)] || 0)
      return bucket(0.5 + rate / (2 * (scales.deltaRate || 0.05)))
    }
    case 'sector':
      // Own colour where this lap owns the cell, muted everywhere else.
      return sectorOwnsSample(entry, index) ? 400 : 401
    default:
      return -1
  }
}

function colorForKey(key, entry) {
  if (key < 0) return entry.color
  if (key === 400) return entry.color
  if (key === 401) return themeColor('--trace-muted')
  if (key === 300) return COAST_COLOR
  if (key >= 200) return palette(THROTTLE_RAMP)[key - 200]
  if (key >= 100) return palette(BRAKE_RAMP)[key - 100]
  const diverging = state.colorMode === 'latg' || state.colorMode === 'delta' ||
    state.colorMode === 'gradient'
  return palette(diverging ? DIVERGING : SPEED_RAMP)[key]
}

// mapProjection fits every selected lap into the canvas, then applies the
// reader's zoom and pan on top of that fit.
// Where the car sits in a heading-up view: low and centred, the way a phone
// navigates, so almost the whole frame is the road ahead. Not flat against the
// bottom — a little of the corner just taken is worth seeing.
const HEADING_ANCHOR_Y = 0.78
// Tilt: a real perspective divide over the ground plane, not a fake squash.
// TILT is the camera pitch away from straight down; CAMERA sets how strong the
// convergence is, as a multiple of the viewport height. Together they put the
// horizon just inside the top of the frame.
const TILT_ANGLE = 55 * Math.PI / 180
const TILT_CAMERA = 0.9
// Ground behind the camera cannot be drawn; those points are pushed far off
// screen so the existing viewport culling lifts the pen over them.
const TILT_MIN_DEPTH = 0.15
// Real relief is small next to a lap's horizontal extent — a 6 m rise over 2 km
// would be a couple of pixels. Terrain views exaggerate for exactly this reason;
// the height is honest, the emphasis is not.
const TILT_HEIGHT_SCALE = 2.5
const OFF_SCREEN = 1e6
// Half-length of the chord the heading is taken from, in metres.
const HEADING_WINDOW = 12
const HEADING_WINDOW_MAX = 40

// pathHeadingAt is the direction of TRAVEL at a sample, taken as the chord
// between points a fixed distance behind and ahead along the lap.
//
// The recorded forward vector is the wrong source here: mid-drift the car points
// somewhere quite else than where it is going, and a spin would spin the whole
// map with it. A chord measured in metres is also immune to standing still,
// where a fixed number of samples covers no ground at all and the angle is pure
// noise.
function pathHeadingAt(entry, index) {
  const channels = entry.lap.channels
  const dist = channels.dist
  const total = dist.length
  if (total < 2) return null

  for (let window = HEADING_WINDOW; window <= HEADING_WINDOW_MAX; window *= 2) {
    let back = index
    while (back > 0 && dist[index] - dist[back] < window) back -= 1
    let ahead = index
    while (ahead < total - 1 && dist[ahead] - dist[index] < window) ahead += 1
    const dx = channels.x[ahead] - channels.x[back]
    const dy = channels.y[ahead] - channels.y[back]
    // A chord this short means the car was parked, reversing, or spinning on the
    // spot: widen the window rather than take the angle of noise.
    if (dx * dx + dy * dy > 4) return Math.atan2(dy, dx)
  }
  return null
}

// mapAnchor is the world point the heading-up view is built around: where the
// cursor is on the reference lap, and which way it was going.
function mapAnchor() {
  if (state.cursorX == null) return null
  const reference = referenceLap()
  const entries = loadedEntries()
  const ordered = reference ? [reference, ...entries.filter((e) => e !== reference)] : entries
  for (const entry of ordered) {
    if (!entry.lap) continue
    const values = axisValues(entry.lap)
    if (!values.length || values[values.length - 1] < state.cursorX) continue
    const index = indexAt(values, state.cursorX)
    if (index < 0) continue
    const heading = pathHeadingAt(entry, index)
    // Holding the last heading is what keeps a spin or a stop from whipping the
    // map around; the position still tracks.
    if (heading != null) state.mapHeading = heading
    return {
      x: entry.lap.channels.x[index],
      y: entry.lap.channels.y[index],
      z: entry.lap.channels.z[index],
      heading: state.mapHeading
    }
  }
  return null
}

function headingUpActive() {
  return state.mapOrientation === 'heading' && mapAnchor() != null
}

function mapProjection(entries, width, height) {
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
  for (const entry of entries) {
    const [x0, y0, x1, y1] = entry.lap.summary.bounds
    minX = Math.min(minX, x0); minY = Math.min(minY, y0)
    maxX = Math.max(maxX, x1); maxY = Math.max(maxY, y1)
  }
  const padding = 26
  const spanX = Math.max(1, maxX - minX)
  const spanY = Math.max(1, maxY - minY)
  const scale = Math.min((width - padding * 2) / spanX, (height - padding * 2) / spanY)
  const offsetX = (width - spanX * scale) / 2
  const offsetY = (height - spanY * scale) / 2
  // BeamNG world Y grows north; canvas Y grows down.
  const view = state.mapView
  const anchor = state.mapOrientation === 'heading' ? mapAnchor() : null
  if (anchor) {
    // Heading up: drop the fit's centring, put the anchor where a navigation
    // view puts you, and turn the world so the direction of travel points up.
    const total = scale * view.scale
    const theta = Math.PI / 2 - anchor.heading
    const cos = Math.cos(theta)
    const sin = Math.sin(theta)
    const originX = width / 2
    const originY = height * HEADING_ANCHOR_Y
    const tilted = state.mapTilt
    const camera = TILT_CAMERA * height
    const tiltSin = Math.sin(TILT_ANGLE)
    const tiltCos = Math.cos(TILT_ANGLE)

    return {
      width,
      height,
      scale: total,
      headingUp: true,
      tilted,
      centreX: originX,
      centreY: originY,
      project: (x, y, z) => {
        const dx = x - anchor.x
        const dy = y - anchor.y
        // Into car space: lateral to the right, forward up the screen.
        const lateral = (dx * cos - dy * sin) * total
        const forward = (dx * sin + dy * cos) * total
        if (!tilted) {
          return [originX + view.panX + lateral, originY + view.panY - forward]
        }
        // Height above the car, which a tilted camera sees: looking straight
        // down it would contribute nothing, looking level it would be all of it,
        // hence sin/cos of the pitch. Raising a point also brings it nearer.
        const height = (z == null ? 0 : (z - anchor.z) * TILT_HEIGHT_SCALE) * total
        const depth = 1 + (forward * tiltSin - height * tiltCos) / camera
        if (depth < TILT_MIN_DEPTH) return [OFF_SCREEN, OFF_SCREEN]
        return [
          originX + view.panX + lateral / depth,
          originY + view.panY - (forward * tiltCos + height * tiltSin) / depth
        ]
      }
    }
  }

  const centreX = width / 2
  const centreY = height / 2
  return {
    // The zoom handler pins a point under the pointer, so it has to use the very
    // same dimensions this fit used: clientWidth is rounded, a bounding rect is
    // not, and half of that difference shows up as drift on every wheel step.
    width,
    height,
    scale: scale * view.scale,
    headingUp: false,
    centreX,
    centreY,
    project: (x, y) => {
      const baseX = offsetX + (x - minX) * scale
      const baseY = height - (offsetY + (y - minY) * scale)
      return [
        (baseX - centreX) * view.scale + centreX + view.panX,
        (baseY - centreY) * view.scale + centreY + view.panY
      ]
    }
  }
}

function setMapOrientation(mode) {
  state.mapOrientation = mode
  writeSetting(ORIENTATION_STORAGE, mode)
  // Any pan carried over from north-up would offset the anchor.
  state.mapView.panX = 0
  state.mapView.panY = 0
  updateZoomControls()
  scheduleRedraw()
}

function setShowRoads(on) {
  state.showRoads = on
  writeSetting(ROADS_STORAGE, on ? '1' : '0')
  updateZoomControls()
  ensureRoads()
  scheduleRedraw()
}

function setMapTilt(on) {
  state.mapTilt = on
  writeSetting(TILT_STORAGE, on ? '1' : '0')
  updateZoomControls()
  scheduleRedraw()
}

function resetMapView() {
  state.mapView = { scale: 1, panX: 0, panY: 0 }
  updateZoomControls()
  scheduleRedraw()
}

function resetRange() {
  state.xRange = null
  updateZoomControls()
  scheduleRedraw()
}

function updateZoomControls() {
  const orient = el('mapOrient')
  const headingUp = state.mapOrientation === 'heading'
  orient.textContent = headingUp ? '▲' : 'N'
  orient.classList.toggle('active', headingUp)
  orient.title = headingUp ? t('orientNorth') : t('orientHeading')

  const roadsButton = el('mapRoads')
  roadsButton.classList.toggle('active', state.showRoads)
  roadsButton.title = state.showRoads ? t('roadsOff') : t('roadsOn')

  // Tilt only means anything once the map is facing the way the car is going.
  const tilt = el('mapTilt')
  tilt.hidden = !headingUp
  tilt.classList.toggle('active', state.mapTilt)
  tilt.title = state.mapTilt ? t('tiltOff') : t('tiltOn')

  const mapReset = el('mapReset')
  mapReset.hidden = state.mapView.scale <= 1.001
  const rangeReset = el('rangeReset')
  const zoomed = isZoomed()
  rangeReset.hidden = !zoomed
  if (zoomed) {
    const window = axisWindow()
    const unit = state.axis === 'dist' ? 'm' : 's'
    const digits = state.axis === 'dist' ? 0 : 2
    rangeReset.textContent =
      `${t('resetRange')} ${window.from.toFixed(digits)}–${window.to.toFixed(digits)} ${unit}`
  }
}

// projectEntries caches screen coordinates per lap. The hover search and the
// cursor dots then read plain typed arrays instead of re-projecting.
function projectEntries(entries, projection) {
  return entries.map((entry) => {
    const channels = entry.lap.channels
    const total = channels.x.length
    const points = new Float32Array(total * 2)
    for (let i = 0; i < total; i += 1) {
      const [px, py] = projection.project(channels.x[i], channels.y[i], channels.z[i])
      points[i * 2] = px
      points[i * 2 + 1] = py
    }
    return { entry, points, total }
  })
}

function drawMap() {
  const canvas = el('map')
  const entries = loadedEntries()
  el('mapEmpty').hidden = entries.length > 0
  const { context, width, height, ratio } = fitCanvas(canvas)
  context.clearRect(0, 0, width, height)
  if (!entries.length) {
    el('mapLegend').textContent = ''
    state.mapProjected = null
    return
  }

  const view = state.mapView
  // Heading up, the whole picture turns and slides with the cursor, so the
  // cached layer is keyed on the anchor as well. Quantizing keeps a jittering
  // last digit from forcing a repaint that changes nothing visible.
  const anchor = state.mapOrientation === 'heading' ? mapAnchor() : null
  const roadsKey = state.showRoads && state.roads ? `r${state.roads.key}` : 'r0'
  const orientationKey = anchor
    ? `h${anchor.heading.toFixed(3)}:${anchor.x.toFixed(1)}:${anchor.y.toFixed(1)}:${state.mapTilt ? 't' : 'f'}`
    : 'n'
  const key = [width, height, state.colorMode, state.referenceKey, state.lang, effectiveTheme(),
    view.scale.toFixed(3), view.panX.toFixed(1), view.panY.toFixed(1), orientationKey, roadsKey,
    state.sectors ? state.sectors.signature : '', selectionSignature()].join('~')
  const scales = colorScales()
  if (mapLayer.key !== key || mapLayer.width !== width || mapLayer.height !== height || mapLayer.ratio !== ratio) {
    const projection = mapProjection(entries, width, height)
    state.mapProjection = projection
    state.mapProjected = projectEntries(entries, projection)
  }
  const buffer = ensureLayer(mapLayer, width, height, ratio, key, (target) => {
    // Roads first: the racing line belongs on top of the tarmac, not under it.
    paintRoads(target, state.mapProjection)
    if (state.colorMode === 'sector') {
      // Everyone's line in grey first, then each lap's winning stretches on top:
      // otherwise the last lap drawn buries the ownership of the ones before it.
      for (const projected of state.mapProjected) paintTrace(target, projected, scales, 'muted')
      for (const projected of state.mapProjected) paintTrace(target, projected, scales, 'owned')
    } else {
      for (const projected of state.mapProjected) paintTrace(target, projected, scales)
    }
    paintGates(target, state.mapProjection, entries)
  })

  context.drawImage(buffer, 0, 0, width, height)
  drawSelectionOnMap(context)
  drawMapCursor(context)
  drawSelectionBox(context)
  drawGauges(context, width, height)
  renderLegend(scales)
}

// visibleAt keeps a point that is on screen or just outside it, so a segment
// crossing the viewport is still drawn while the rest costs nothing.
function visibleAt(points, index, width, height) {
  const x = points[index * 2]
  const y = points[index * 2 + 1]
  return x > -60 && x < width + 60 && y > -60 && y < height + 60
}

function paintTrace(context, projected, scales, pass) {
  const { entry, points, total } = projected
  const width = mapLayer.width
  const height = mapLayer.height
  const reference = referenceLap()
  const isReference = reference && lapKey(reference) === lapKey(entry)
  const flat = state.colorMode === 'lap' ||
    (state.colorMode === 'delta' && isReference) ||
    pass === 'muted'

  context.lineWidth = pass === 'muted' ? 1.4 : (isReference ? 2.6 : 1.9)
  context.lineJoin = 'round'
  context.lineCap = 'round'

  if (pass === 'owned') {
    paintOwnedStretches(context, projected)
    return
  }

  if (flat) {
    context.strokeStyle = pass === 'muted'
      ? themeColor('--trace-muted')
      : (state.colorMode === 'delta' && isReference
        ? themeColor('--chart-reference-dim')
        : entry.color)
    context.beginPath()
    context.moveTo(points[0], points[1])
    let lastX = points[0]
    let lastY = points[1]
    let wasVisible = visibleAt(points, 0, width, height)
    for (let i = 1; i < total; i += 1) {
      const x = points[i * 2]
      const y = points[i * 2 + 1]
      const visible = visibleAt(points, i, width, height)
      if (!visible && !wasVisible) {
        // Both ends off screen: lift the pen instead of drawing into the void.
        context.moveTo(x, y)
        lastX = x
        lastY = y
        wasVisible = visible
        continue
      }
      wasVisible = visible
      // Sub-pixel steps are invisible but cost a line segment each.
      if (i < total - 1 && (x - lastX) ** 2 + (y - lastY) ** 2 < 0.8) continue
      context.lineTo(x, y)
      lastX = x
      lastY = y
    }
    context.stroke()
    return
  }

  let runKey = pointColorKey(entry, 0, scales)
  let lastX = points[0]
  let lastY = points[1]
  let wasVisible = visibleAt(points, 0, width, height)
  context.beginPath()
  context.moveTo(lastX, lastY)
  for (let i = 1; i < total; i += 1) {
    const x = points[i * 2]
    const y = points[i * 2 + 1]
    const visible = visibleAt(points, i, width, height)
    if (!visible && !wasVisible) {
      context.moveTo(x, y)
      lastX = x
      lastY = y
      continue
    }
    wasVisible = visible
    const key = pointColorKey(entry, i, scales)
    const far = (x - lastX) ** 2 + (y - lastY) ** 2 >= 0.8
    if (key === runKey) {
      if (!far && i < total - 1) continue
      context.lineTo(x, y)
    } else {
      // Close the run on the new point so the colours meet without a gap.
      context.lineTo(x, y)
      context.strokeStyle = colorForKey(runKey, entry)
      context.stroke()
      context.beginPath()
      context.moveTo(x, y)
      runKey = key
    }
    lastX = x
    lastY = y
  }
  context.strokeStyle = colorForKey(runKey, entry)
  context.stroke()
}

// paintOwnedStretches draws just the cells this lap was quickest through.
function paintOwnedStretches(context, projected) {
  const { entry, points, total } = projected
  if (!state.sectors || !entry.stations) return
  context.strokeStyle = entry.color
  context.beginPath()
  let drawing = false
  for (let i = 0; i < total; i += 1) {
    if (sectorOwnsSample(entry, i)) {
      if (!drawing) {
        context.moveTo(points[i * 2], points[i * 2 + 1])
        drawing = true
      } else {
        context.lineTo(points[i * 2], points[i * 2 + 1])
      }
    } else if (drawing) {
      // Carry one point past the boundary so neighbouring stretches meet.
      context.lineTo(points[i * 2], points[i * 2 + 1])
      drawing = false
    }
  }
  context.stroke()
}

function paintGates(context, projection, entries) {
  const startLine = entries.map((entry) => entry.lap.startLine).find(Boolean)
  const library = state.activeLibrary
  if (startLine && startLine.position) {
    paintGate(context, projection, startLine.position, startLine.normal, startLine.halfWidth || 8, '#3ddc97', 'START')
  } else if (library && library.position) {
    paintGate(context, projection, library.position, [0, 1, 0], 8, '#3ddc97', 'START')
  }
  if (library && library.finishPosition) {
    paintGate(context, projection, library.finishPosition, [1, 0, 0], 8, '#ffd23d', 'FINISH')
  }
}

function paintGate(context, projection, position, normal, halfWidth, color, label) {
  const nx = normal[0] || 0
  const ny = normal[1] || 0
  const length = Math.hypot(nx, ny) || 1
  // The gate line is perpendicular to the normal a car crosses it along.
  const tx = -ny / length
  const ty = nx / length
  const [ax, ay] = projection.project(position[0] - tx * halfWidth, position[1] - ty * halfWidth)
  const [bx, by] = projection.project(position[0] + tx * halfWidth, position[1] + ty * halfWidth)
  context.strokeStyle = color
  context.lineWidth = 2
  context.setLineDash([4, 3])
  context.beginPath()
  context.moveTo(ax, ay)
  context.lineTo(bx, by)
  context.stroke()
  context.setLineDash([])
  context.fillStyle = color
  context.font = '10px ui-monospace, monospace'
  context.fillText(label, bx + 4, by)
}

// drawSelectionOnMap marks the selected stretch WITHOUT repainting it in a flat
// colour: the trace keeps whatever the colour mode is showing there — which is
// the whole point of looking at that stretch — and the selection reads as extra
// width plus a tick at each end.
function drawSelectionOnMap(context) {
  if (!state.selection || !state.mapProjected) return
  const scales = colorScales()
  for (const projected of state.mapProjected) {
    // Sector stretches are measured along the reference path, so highlight by
    // station where it is known: a lap on a wider line would otherwise have its
    // stretch shifted by the few metres its own odometer runs ahead.
    const values = state.axis === 'dist' && projected.entry.stations
      ? projected.entry.stations
      : axisValues(projected.entry.lap)
    let first = -1
    let last = -1
    for (let i = 0; i < projected.total; i += 1) {
      if (values[i] < state.selection.from || values[i] > state.selection.to) continue
      if (first < 0) first = i
      last = i
    }
    if (first < 0 || last <= first) continue
    strokeRangeInDataColours(context, projected, scales, first, last, 5)
    drawStretchTick(context, projected, first)
    drawStretchTick(context, projected, last)
  }
}

// strokeRangeInDataColours redraws part of a trace at a given width, keeping the
// per-sample colouring the current mode produces (the same run batching the
// static layer uses, so a long stretch is still a handful of strokes).
function strokeRangeInDataColours(context, projected, scales, first, last, lineWidth) {
  const { entry, points } = projected
  const flat = state.colorMode === 'lap'
  context.lineWidth = lineWidth
  context.lineJoin = 'round'
  context.lineCap = 'round'

  if (flat) {
    context.strokeStyle = entry.color
    context.beginPath()
    context.moveTo(points[first * 2], points[first * 2 + 1])
    for (let i = first + 1; i <= last; i += 1) context.lineTo(points[i * 2], points[i * 2 + 1])
    context.stroke()
    return
  }

  let runKey = pointColorKey(entry, first, scales)
  context.beginPath()
  context.moveTo(points[first * 2], points[first * 2 + 1])
  for (let i = first + 1; i <= last; i += 1) {
    const x = points[i * 2]
    const y = points[i * 2 + 1]
    const key = pointColorKey(entry, i, scales)
    context.lineTo(x, y)
    if (key !== runKey) {
      context.strokeStyle = colorForKey(runKey, entry)
      context.stroke()
      context.beginPath()
      context.moveTo(x, y)
      runKey = key
    }
  }
  context.strokeStyle = colorForKey(runKey, entry)
  context.stroke()
}

// drawStretchTick puts a short bar across the line at a stretch boundary, so
// where the selection starts and ends is readable without recolouring anything.
function drawStretchTick(context, projected, index) {
  const { points, total } = projected
  const other = index + 1 < total ? index + 1 : index - 1
  if (other < 0) return
  const dx = points[other * 2] - points[index * 2]
  const dy = points[other * 2 + 1] - points[index * 2 + 1]
  const length = Math.hypot(dx, dy) || 1
  const nx = -dy / length
  const ny = dx / length
  const reach = 7
  context.strokeStyle = themeColor('--chart-cursor-pinned')
  context.lineWidth = 2
  context.beginPath()
  context.moveTo(points[index * 2] - nx * reach, points[index * 2 + 1] - ny * reach)
  context.lineTo(points[index * 2] + nx * reach, points[index * 2 + 1] + ny * reach)
  context.stroke()
}

function drawSelectionBox(context) {
  const gesture = state.selecting
  if (!gesture || gesture.pane !== 'map') return
  const left = Math.min(gesture.x0, gesture.x1)
  const top = Math.min(gesture.y0, gesture.y1)
  const width = Math.abs(gesture.x1 - gesture.x0)
  const height = Math.abs(gesture.y1 - gesture.y0)
  context.strokeStyle = themeColor('--accent')
  context.lineWidth = 1
  context.setLineDash([4, 3])
  context.strokeRect(left + 0.5, top + 0.5, width, height)
  context.setLineDash([])
}

const GAUGE_CHANNELS = [
  { key: 'throttle', letter: 'T', color: '#3ddc97' },
  { key: 'brake', letter: 'B', color: '#ff4d4d' },
  { key: 'handbrake', letter: 'H', color: '#ffd23d' }
]

// drawGauges puts the in-game style vertical bars in the map's bottom-right
// corner: one cluster per lap, throttle/brake/handbrake and the gear. The speed
// legend keeps the bottom-left corner to itself.
function drawGauges(context, width, height) {
  if (state.cursorX == null || !state.mapProjected) return
  const entries = loadedEntries().filter((entry) => entry.lap.hasInputs).slice(0, 4)
  if (!entries.length) return

  const barWidth = 7
  const barGap = 4
  const padding = 7
  const barsHeight = 54
  const clusterWidth = barWidth * GAUGE_CHANNELS.length + barGap * (GAUGE_CHANNELS.length - 1) + padding * 2
  const boxHeight = barsHeight + 34
  const boxWidth = clusterWidth * entries.length
  const boxLeft = width - boxWidth - 12
  const boxTop = height - boxHeight - 12
  if (boxLeft < 8 || boxTop < 8) return

  context.fillStyle = themeColor('--overlay')
  context.strokeStyle = themeColor('--line')
  context.lineWidth = 1
  context.beginPath()
  context.rect(boxLeft + 0.5, boxTop + 0.5, boxWidth, boxHeight)
  context.fill()
  context.stroke()

  context.textAlign = 'center'
  context.font = '9px ui-monospace, monospace'

  entries.forEach((entry, column) => {
    const channels = entry.lap.channels
    const values = axisValues(entry.lap)
    const own = lapCursorValue(entry)
    const index = indexAt(values, own)
    const ended = index < 0 || values[values.length - 1] < own
    const clusterLeft = boxLeft + column * clusterWidth + padding
    const top = boxTop + 8

    // The lap's colour identifies the cluster without spending a text row.
    context.fillStyle = entry.color
    context.fillRect(clusterLeft, boxTop + 4, clusterWidth - padding * 2, 2)

    GAUGE_CHANNELS.forEach((channel, i) => {
      const x = clusterLeft + i * (barWidth + barGap)
      context.fillStyle = themeColor('--trace-muted')
      context.fillRect(x, top, barWidth, barsHeight)
      if (!ended) {
        const value = clamp(channels[channel.key] ? channels[channel.key][index] : 0, 0, 1)
        const filled = Math.round(barsHeight * value)
        if (filled > 0) {
          context.fillStyle = channel.color
          context.fillRect(x, top + barsHeight - filled, barWidth, filled)
        }
      }
      context.fillStyle = themeColor('--chart-text')
      context.fillText(channel.letter, x + barWidth / 2, top + barsHeight + 10)
    })

    const gear = ended ? '—' : String(Math.round(channels.gear[index]))
    // The gear takes the lap's colour, which is what ties it to its cluster.
    context.fillStyle = ended ? themeColor('--chart-text') : entry.color
    context.font = 'bold 12px ui-monospace, monospace'
    context.fillText(gear, clusterLeft + (clusterWidth - padding * 2) / 2, top + barsHeight + 23)
    context.font = '9px ui-monospace, monospace'
  })
  context.textAlign = 'left'
}

function drawMapCursor(context) {
  if (state.cursorX == null || !state.mapProjected) return
  for (const { entry, points } of state.mapProjected) {
    const values = axisValues(entry.lap)
    const index = indexAt(values, lapCursorValue(entry))
    if (index < 0) continue
    context.beginPath()
    context.arc(points[index * 2], points[index * 2 + 1], 4.5, 0, Math.PI * 2)
    context.fillStyle = entry.color
    context.fill()
    context.lineWidth = 1.5
    context.strokeStyle = themeColor('--chart-dot-ring')
    context.stroke()
    if (state.cursorPinned) {
      // A halo makes the parked point findable after panning around.
      context.beginPath()
      context.arc(points[index * 2], points[index * 2 + 1], 8.5, 0, Math.PI * 2)
      context.strokeStyle = themeColor('--chart-cursor-pinned')
      context.lineWidth = 1.5
      context.stroke()
    }
  }
}

function renderLegend(scales) {
  const legend = el('mapLegend')
  const labels = {
    speed: [`${kmh(scales.speedMin).toFixed(0)} km/h`, `${kmh(scales.speedMax).toFixed(0)} km/h`, SPEED_RAMP],
    gear: [t('legendGearLow'), t('legendGearHigh'), SPEED_RAMP],
    latg: [`-${scales.latgMax.toFixed(1)} G`, `+${scales.latgMax.toFixed(1)} G`, DIVERGING],
    elevation: [`${scales.elevationMin.toFixed(0)} m`,
      `${(scales.elevationMin + scales.elevationSpan).toFixed(0)} m`, SPEED_RAMP],
    gradient: [`-${scales.gradientMax.toFixed(1)}%`, `+${scales.gradientMax.toFixed(1)}%`, DIVERGING],
    delta: [t('legendDeltaGain'), t('legendDeltaLoss'), DIVERGING],
    throttle: [t('legendBrake'), t('legendThrottle'), ['#ff2d2d', COAST_COLOR, '#3ddc97']],
    sector: null,
    lap: null
  }
  const definition = labels[state.colorMode]
  const signature = definition ? `${state.lang}:${state.colorMode}:${definition[0]}:${definition[1]}` : 'none'
  legend.hidden = !definition
  if (legend.dataset.signature === signature) return
  legend.dataset.signature = signature
  legend.textContent = ''
  if (!definition) return

  const [low, high, ramp] = definition
  const bar = document.createElement('span')
  bar.className = 'bar'
  bar.style.background = `linear-gradient(90deg, ${ramp.join(',')})`
  const lowLabel = document.createElement('span')
  lowLabel.textContent = low
  const highLabel = document.createElement('span')
  highLabel.textContent = high
  legend.append(lowLabel, bar, highLabel)
}

/* ---------------------------------------------------------------- charts */

const CHART_DEFS = [
  {
    id: 'speed', labelKey: 'chartSpeed', height: 118,
    series: (entry) => [{ values: cached(entry, 'speed', (lap) => lap.channels.speed.map(kmh)), color: entry.color }]
  },
  {
    id: 'delta', labelKey: 'chartDelta', height: 108, zeroLine: true,
    enabled: () => loadedEntries().length > 1,
    series: (entry) => (entry.delta ? [{ values: entry.delta, color: entry.color }] : [])
  },
  {
    id: 'inputs', labelKey: 'chartInputs', height: 104, domain: [0, 100],
    enabled: () => state.selected.some((entry) => entry.lap && entry.lap.hasInputs),
    series: (entry) => (entry.lap.hasInputs ? [
      { values: cached(entry, 'throttle', (lap) => lap.channels.throttle.map((v) => v * 100)), color: entry.color },
      { values: cached(entry, 'brake', (lap) => lap.channels.brake.map((v) => v * 100)), color: entry.color, dash: [3, 3] }
    ] : [])
  },
  {
    id: 'latg', labelKey: 'chartLatG', height: 96, zeroLine: true,
    series: (entry) => [{ values: entry.lap.channels.latG, color: entry.color }]
  },
  {
    id: 'accel', labelKey: 'chartAccel', height: 96, zeroLine: true,
    series: (entry) => [{ values: entry.lap.channels.accel, color: entry.color }]
  },
  {
    id: 'elevation', labelKey: 'chartElevation', height: 90,
    series: (entry) => [{ values: entry.lap.channels.z, color: entry.color }]
  },
  {
    id: 'gradient', labelKey: 'chartGradient', height: 90, zeroLine: true,
    enabled: () => state.selected.some((entry) => entry.lap && entry.lap.channels.gradient),
    series: (entry) => (entry.lap.channels.gradient
      ? [{ values: entry.lap.channels.gradient, color: entry.color }]
      : [])
  },
  {
    id: 'gear', labelKey: 'chartGear', height: 84, step: true,
    enabled: () => state.selected.some((entry) => entry.lap && entry.lap.hasInputs),
    series: (entry) => (entry.lap.hasInputs ? [{ values: entry.lap.channels.gear, color: entry.color }] : [])
  }
]

// cached memoizes a derived series on the selection entry; channel data never
// changes once loaded, so this survives every cursor redraw.
function cached(entry, name, build) {
  entry.cache = entry.cache || {}
  if (!entry.cache[name]) entry.cache[name] = build(entry.lap)
  return entry.cache[name]
}

function activeCharts() {
  return CHART_DEFS.filter((definition) => !definition.enabled || definition.enabled())
}

function buildCharts() {
  const container = el('charts')
  const definitions = activeCharts()
  const signature = definitions.map((definition) => definition.id).join(',')
  if (container.dataset.signature === signature) return
  container.dataset.signature = signature
  container.textContent = ''
  state.charts = definitions.map((definition) => {
    const wrapper = document.createElement('div')
    wrapper.className = 'chart'
    const canvas = document.createElement('canvas')
    wrapper.append(canvas)
    container.append(wrapper)
    attachCursor(canvas)
    attachChartZoom(canvas)
    return { definition, canvas, layer: { canvas: null, key: '', width: 0, height: 0, ratio: 0 } }
  })
}

function drawCharts() {
  const entries = loadedEntries()
  const window = axisWindow()
  for (let i = 0; i < state.charts.length; i += 1) {
    drawChart(state.charts[i], entries, window, i === state.charts.length - 1)
  }
}

function drawChart(chart, entries, window, isLast) {
  const { definition, canvas } = chart
  const { context, width, height, ratio } = fitCanvas(canvas, definition.height)
  context.clearRect(0, 0, width, height)

  // Only the Δt chart is drawn against the reference lap.
  const reference = definition.id === 'delta'
    ? `${state.referenceKey}|${state.sectors ? state.sectors.signature : ''}`
    : ''
  const key = [definition.id, width, height, state.axis, window.from, window.to, isLast, reference,
    state.lang, effectiveTheme(), selectionSignature()].join('~')
  if (chart.layer.key !== key || chart.layer.width !== width || chart.layer.height !== height) {
    prepareChart(chart, entries, window, isLast, width, height)
  }
  const buffer = ensureLayer(chart.layer, width, height, ratio, key, (target) => paintChart(target, chart, isLast))
  context.drawImage(buffer, 0, 0, width, height)
  drawChartSelection(context, chart)
  drawChartCursor(context, chart)
}

// prepareChart resolves the series and the value domain once, so a cursor move
// never walks the samples again.
function prepareChart(chart, entries, window, isLast, width, height) {
  const { definition } = chart
  const left = 46
  const right = 8
  const top = 16
  const bottom = isLast ? 20 : 10
  const plotWidth = Math.max(1, width - left - right)
  const plotHeight = Math.max(1, height - top - bottom)

  const series = []
  for (const entry of entries) {
    for (const item of definition.series(entry)) {
      if (item.values && item.values.length) series.push({ ...item, entry })
    }
  }

  let low = definition.domain ? definition.domain[0] : Infinity
  let high = definition.domain ? definition.domain[1] : -Infinity
  if (!definition.domain) {
    // Only what is on screen sets the scale, so zooming into a corner actually
    // magnifies it instead of leaving it flat against a whole-lap range.
    for (const item of series) {
      const axis = axisValues(item.entry.lap)
      for (let i = 0; i < item.values.length; i += 1) {
        if (axis[i] < window.from || axis[i] > window.to) continue
        const value = item.values[i]
        if (!isFinite(value)) continue
        if (value < low) low = value
        if (value > high) high = value
      }
    }
    if (!isFinite(low)) { low = 0; high = 1 }
    if (definition.zeroLine) {
      const reach = Math.max(Math.abs(low), Math.abs(high)) || 1
      low = -reach
      high = reach
    }
    const pad = (high - low) * 0.08 || 1
    low -= pad
    high += pad
  }

  const span = window.to - window.from || 1
  chart.series = series
  chart.geometry = {
    left, top, plotWidth, plotHeight, low, high,
    from: window.from, to: window.to, span,
    xAt: (value) => left + ((value - window.from) / span) * plotWidth,
    yAt: (value) => top + plotHeight - ((value - low) / (high - low || 1)) * plotHeight
  }
}

function paintChart(context, chart, isLast) {
  const { definition, series, geometry } = chart
  const { left, top, plotWidth, plotHeight, from, span, low, high } = geometry

  context.strokeStyle = themeColor('--chart-grid')
  context.lineWidth = 1
  context.beginPath()
  for (let i = 0; i <= 4; i += 1) {
    const y = Math.round(top + (plotHeight * i) / 4) + 0.5
    context.moveTo(left, y)
    context.lineTo(left + plotWidth, y)
  }
  for (let i = 0; i <= 6; i += 1) {
    const x = Math.round(left + (plotWidth * i) / 6) + 0.5
    context.moveTo(x, top)
    context.lineTo(x, top + plotHeight)
  }
  context.stroke()

  context.fillStyle = themeColor('--chart-text')
  context.font = '10px ui-monospace, monospace'
  context.textAlign = 'left'
  context.fillText(t(definition.labelKey), left, 11)
  context.textAlign = 'right'
  context.fillText(formatAxisValue(high), left - 6, top + 8)
  context.fillText(formatAxisValue(low), left - 6, top + plotHeight)
  context.fillText(formatAxisValue((low + high) / 2), left - 6, top + plotHeight / 2 + 3)

  if (definition.zeroLine && low < 0 && high > 0) {
    context.strokeStyle = themeColor('--chart-zero')
    context.beginPath()
    const zero = Math.round(geometry.yAt(0)) + 0.5
    context.moveTo(left, zero)
    context.lineTo(left + plotWidth, zero)
    context.stroke()
  }

  if (isLast) {
    context.textAlign = 'center'
    for (let i = 0; i <= 6; i += 1) {
      const value = from + (span * i) / 6
      const text = state.axis === 'dist'
        ? `${value.toFixed(0)}m`
        : `${value.toFixed(span < 20 ? 2 : 1)}s`
      context.fillText(text, left + (plotWidth * i) / 6, top + plotHeight + 14)
    }
  }

  if (definition.id === 'delta') paintSectorStrip(context, chart)

  context.lineWidth = 1.5
  // Bevel joins on a near-vertical envelope look identical to round ones and
  // cost far less to rasterize; a lap can contribute thousands of joins.
  context.lineJoin = 'bevel'
  context.lineCap = 'butt'
  // Clip to the plot: zoomed in, a series carries samples just outside the
  // window so its line enters and leaves correctly, and a held value (the gear
  // trace especially) would otherwise run out over the axis labels.
  context.save()
  context.beginPath()
  context.rect(left, top, plotWidth, plotHeight)
  context.clip()
  for (const item of series) paintSeries(context, item, chart)
  context.restore()
  context.setLineDash([])
}

// paintSectorStrip marks who owned each stretch along the top of the Δt chart,
// which is exactly where the reader is already looking for time gained or lost.
function paintSectorStrip(context, chart) {
  const sectors = state.sectors
  if (!sectors || state.axis !== 'dist') return
  const { left, top, plotWidth, xAt } = chart.geometry
  const entries = loadedEntries()
  for (let k = 0; k < sectors.cells; k += 1) {
    const index = sectors.owner[k]
    if (index < 0 || !entries[index]) continue
    const from = xAt(k * sectors.step)
    const to = xAt((k + 1) * sectors.step)
    if (to < left || from > left + plotWidth) continue
    const start = Math.max(left, from)
    const end = Math.min(left + plotWidth, to)
    context.fillStyle = entries[index].color
    context.fillRect(start, top - 6, Math.max(1, end - start), 4)
  }
}

// paintSeries draws one channel. When a lap carries more samples than the plot
// has pixels, each column is reduced to its min/max — that keeps braking spikes
// visible, which plain stride sampling drops.
function paintSeries(context, item, chart) {
  const { definition, geometry } = chart
  const { left, plotWidth, from, span, xAt, yAt } = geometry
  const axis = axisValues(item.entry.lap)
  const total = Math.min(axis.length, item.values.length)
  context.strokeStyle = item.color
  context.setLineDash(item.dash || [])
  context.beginPath()

  const columns = Math.max(1, Math.round(plotWidth))
  if (!definition.step && total > columns * 2) {
    const minima = new Float32Array(columns).fill(Infinity)
    const maxima = new Float32Array(columns).fill(-Infinity)
    for (let i = 0; i < total; i += 1) {
      const value = item.values[i]
      if (!isFinite(value)) continue
      const column = Math.floor(((axis[i] - from) / span) * columns)
      // Samples outside the window are off screen; they must not squeeze into
      // the edge columns and bend the first and last segment.
      if (column < 0 || column >= columns) continue
      if (value < minima[column]) minima[column] = value
      if (value > maxima[column]) maxima[column] = value
    }
    let started = false
    for (let column = 0; column < columns; column += 1) {
      if (minima[column] === Infinity) continue
      const x = left + column
      const high = yAt(maxima[column])
      const low = yAt(minima[column])
      if (!started) {
        context.moveTo(x, high)
        started = true
      } else {
        context.lineTo(x, high)
      }
      // Only spend a second vertex where the column really spans some height.
      if (low - high > 0.75) context.lineTo(x, low)
    }
    context.stroke()
    return
  }

  const stride = Math.max(1, Math.floor(total / (columns * 2)))
  let previousY = null
  for (let i = 0; i < total; i += stride) {
    const value = item.values[i]
    if (!isFinite(value)) continue
    if (axis[i] < from - span || axis[i] > from + span * 2) continue
    const x = xAt(axis[i])
    const y = yAt(value)
    if (previousY == null) {
      context.moveTo(x, y)
    } else if (definition.step) {
      // Gears hold until they change; a sloped line would invent shifts.
      context.lineTo(x, previousY)
      context.lineTo(x, y)
    } else {
      context.lineTo(x, y)
    }
    previousY = y
  }
  context.stroke()
}

// drawChartSelection shades the selected stretch. It runs whether or not a
// cursor is set, and while the gesture is still being dragged.
function drawChartSelection(context, chart) {
  const { geometry } = chart
  if (!geometry) return
  const range = state.selecting && state.selecting.pane === 'chart'
    ? {
      from: Math.min(state.selecting.from, state.selecting.to),
      to: Math.max(state.selecting.from, state.selecting.to)
    }
    : state.selection
  if (!range) return
  const { left, top, plotWidth, plotHeight, xAt } = geometry
  const bandLeft = clamp(xAt(range.from), left, left + plotWidth)
  const bandRight = clamp(xAt(range.to), left, left + plotWidth)
  context.fillStyle = themeColor('--accent')
  context.globalAlpha = 0.12
  context.fillRect(bandLeft, top, Math.max(1, bandRight - bandLeft), plotHeight)
  context.globalAlpha = 1
}

function drawChartCursor(context, chart) {
  const { geometry, series } = chart
  if (!geometry || state.cursorX == null) return
  const { left, top, plotWidth, plotHeight, from, span, xAt, yAt } = geometry
  if (state.cursorX < from || state.cursorX > from + span) return

  const x = Math.round(xAt(state.cursorX)) + 0.5
  context.strokeStyle = state.cursorPinned
    ? themeColor('--chart-cursor-pinned')
    : themeColor('--chart-cursor')
  context.lineWidth = state.cursorPinned ? 1.5 : 1
  context.beginPath()
  context.moveTo(x, top)
  context.lineTo(x, top + plotHeight)
  context.stroke()

  context.save()
  context.beginPath()
  context.rect(left, top, plotWidth, plotHeight)
  context.clip()
  for (const item of series) {
    const axis = axisValues(item.entry.lap)
    const own = lapCursorValue(item.entry)
    if (axis[axis.length - 1] < own) continue
    const index = indexAt(axis, own)
    if (index < 0 || !isFinite(item.values[index])) continue
    context.fillStyle = item.color
    context.beginPath()
    context.arc(xAt(axis[index]), yAt(item.values[index]), 2.6, 0, Math.PI * 2)
    context.fill()
  }
  context.restore()
}

function formatAxisValue(value) {
  if (Math.abs(value) >= 100) return value.toFixed(0)
  if (Math.abs(value) >= 10) return value.toFixed(1)
  return value.toFixed(2)
}

/* ---------------------------------------------------------------- cursor */

function chartValueAt(canvas, clientX) {
  const chart = state.charts.find((item) => item.canvas === canvas)
  if (!chart || !chart.geometry) return null
  const rect = canvas.getBoundingClientRect()
  const { left, plotWidth, from, span } = chart.geometry
  const ratio = (clientX - rect.left - left) / plotWidth
  return Math.max(from, Math.min(from + span, from + ratio * span))
}

function attachCursor(canvas) {
  canvas.addEventListener('mousemove', (event) => {
    if (state.chartDrag) return
    const value = chartValueAt(canvas, event.clientX)
    if (value != null) setCursor(value, { source: 'chart' })
  })
  canvas.addEventListener('mouseleave', () => {
    if (!cursorLocked()) setCursor(null, { source: 'chart' })
  })
  canvas.addEventListener('click', (event) => {
    // A click that ended a pan is not a click.
    if (state.dragMoved) return
    const value = chartValueAt(canvas, event.clientX)
    if (value == null) return
    setCursor(state.cursorPinned && Math.abs(value - state.cursorX) < 1e-9 ? null : value,
      { source: 'chart', pin: true })
  })
}

function attachMapCursor() {
  const canvas = el('map')
  canvas.addEventListener('mousemove', (event) => {
    if (!state.mapProjected || state.mapDrag) return
    const rect = canvas.getBoundingClientRect()
    const pointerX = event.clientX - rect.left
    const pointerY = event.clientY - rect.top
    let best = null
    // Coordinates were projected once when the static layer was built, so this
    // is a scan over cached floats: coarse pass, then a refine around the hit.
    for (const { entry, points, total } of state.mapProjected) {
      let bestIndex = -1
      let bestDistance = Infinity
      const coarse = Math.max(1, Math.floor(total / 400))
      for (let i = 0; i < total; i += coarse) {
        const dx = points[i * 2] - pointerX
        const dy = points[i * 2 + 1] - pointerY
        const distance = dx * dx + dy * dy
        if (distance < bestDistance) { bestDistance = distance; bestIndex = i }
      }
      for (let i = Math.max(0, bestIndex - coarse); i < Math.min(total, bestIndex + coarse); i += 1) {
        const dx = points[i * 2] - pointerX
        const dy = points[i * 2 + 1] - pointerY
        const distance = dx * dx + dy * dy
        if (distance < bestDistance) { bestDistance = distance; bestIndex = i }
      }
      if (bestIndex >= 0 && bestDistance < (best ? best.distance : Infinity)) {
        best = { entry, index: bestIndex, distance: bestDistance }
      }
    }
    if (!best || best.distance > 40 ** 2) return
    state.hoverLapKey = lapKey(best.entry)
    state.mapHoverValue = axisValues(best.entry.lap)[best.index]
    setCursor(state.mapHoverValue, { source: 'map' })
  })
  canvas.addEventListener('mouseleave', () => {
    if (!cursorLocked()) setCursor(null, { source: 'map' })
  })
  canvas.addEventListener('click', () => {
    if (state.dragMoved || state.mapHoverValue == null) return
    setCursor(state.mapHoverValue, { source: 'map', pin: true })
  })
}

/* ------------------------------------------------------- select and play */

// The selection is held in the current axis's units, but playback runs on the
// reference lap's clock: these two convert between them.
function axisToTime(value) {
  const reference = referenceLap()
  if (!reference || !reference.lap) return null
  if (state.axis === 'time') return value
  return interpolate(reference.lap.channels.dist, reference.lap.channels.t, value)
}

function timeToAxis(time) {
  const reference = referenceLap()
  if (!reference || !reference.lap) return null
  if (state.axis === 'time') return time
  return interpolate(reference.lap.channels.t, reference.lap.channels.dist, time)
}

// disengagePlayback drops the raced-apart positions and puts every lap back on
// the same point of track, which is what the cursor means outside a replay.
function disengagePlayback() {
  if (!state.playback.engaged) return
  state.playback.engaged = false
  for (const entry of state.selected) entry.playbackStart = null
}

function setSelection(from, to) {
  const full = axisMax()
  const low = clamp(Math.min(from, to), 0, full)
  const high = clamp(Math.max(from, to), 0, full)
  // A stray click should not leave a zero-width selection behind.
  state.selection = high - low < full / 500 ? null : { from: low, to: high }
  stopPlayback()
  disengagePlayback()
  updatePlaybackControls()
  scheduleRedraw()
}

function clearSelection() {
  state.selection = null
  state.selectedRunKey = null
  stopPlayback()
  disengagePlayback()
  updatePlaybackControls()
  scheduleRedraw()
}

// selectionOnMap turns a rubber-banded box into the stretch of the reference
// lap that runs through it, so a box drawn over a corner selects that corner.
function selectionOnMap(box) {
  const reference = referenceLap()
  const projected = state.mapProjected &&
    state.mapProjected.find((item) => reference && lapKey(item.entry) === lapKey(reference))
  const target = projected || (state.mapProjected && state.mapProjected[0])
  if (!target) return
  const values = axisValues(target.entry.lap)
  let low = Infinity
  let high = -Infinity
  for (let i = 0; i < target.total; i += 1) {
    const x = target.points[i * 2]
    const y = target.points[i * 2 + 1]
    if (x < box.left || x > box.right || y < box.top || y > box.bottom) continue
    if (values[i] < low) low = values[i]
    if (values[i] > high) high = values[i]
  }
  if (!isFinite(low)) return
  setSelection(low, high)
}

// sectorRunToSelection converts a stretch (metres along the reference path)
// into the units the selection and the cursor are expressed in.
function sectorRunToSelection(run) {
  if (state.axis === 'dist') return { from: run.from, to: run.to }
  const reference = referenceLap()
  const anchor = reference && reference.stations ? reference : run.entry
  if (!anchor || !anchor.stations) return null
  return {
    from: interpolate(anchor.stations, anchor.lap.channels.t, run.from),
    to: interpolate(anchor.stations, anchor.lap.channels.t, run.to)
  }
}

// fitMapToSelection frames the selected stretch: a row clicked in the sector
// table carries no spatial context, so the map has to go there itself.
function fitMapToSelection() {
  const projection = state.mapProjection
  if (!state.selection || !projection || !state.mapProjected) return

  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const { entry } of state.mapProjected) {
    const channels = entry.lap.channels
    const values = state.axis === 'dist' && entry.stations ? entry.stations : axisValues(entry.lap)
    for (let i = 0; i < values.length; i += 1) {
      if (values[i] < state.selection.from || values[i] > state.selection.to) continue
      minX = Math.min(minX, channels.x[i])
      maxX = Math.max(maxX, channels.x[i])
      minY = Math.min(minY, channels.y[i])
      maxY = Math.max(maxY, channels.y[i])
    }
  }
  if (!isFinite(minX)) return

  // Leave a quarter of the frame around the stretch so its exits stay visible.
  const usableWidth = projection.width * 0.75
  const usableHeight = projection.height * 0.75
  const desired = Math.min(
    usableWidth / Math.max(1, maxX - minX),
    usableHeight / Math.max(1, maxY - minY)
  )
  const view = state.mapView
  view.scale = clamp(view.scale * (desired / projection.scale), 1, 40)

  // Heading up anchors on the car, so only the scale applies there.
  if (state.mapOrientation === 'heading') {
    scheduleRedraw()
    return
  }
  // Rebuild at the new scale, then slide the stretch's centre to the middle.
  view.panX = 0
  view.panY = 0
  drawMap()
  const centre = state.mapProjection.project((minX + maxX) / 2, (minY + maxY) / 2)
  view.panX += state.mapProjection.width / 2 - centre[0]
  view.panY += state.mapProjection.height / 2 - centre[1]
  updateZoomControls()
  scheduleRedraw()
}

function selectSectorRun(run, play) {
  const range = sectorRunToSelection(run)
  if (!range) return
  setSelection(range.from, range.to)
  state.selectedRunKey = `${run.entry.id}:${run.from}:${run.to}`
  renderSummary()
  fitMapToSelection()
  if (play) {
    state.playback.time = axisToTime(range.from)
    startPlayback()
    updatePlaybackControls()
  }
}

// stretchEntryTime is when THIS lap reached the start of the stretch, on its own
// clock. Laps cross a given point seconds apart, so a replay that puts them all
// at the same distance shows them stacked on top of each other and hides the
// very thing being compared.
function stretchEntryTime(entry, from) {
  const reference = referenceLap()
  let station = from
  if (state.axis === 'time') {
    if (!reference || !reference.lap) return null
    const referenceStations = reference.stations || reference.lap.channels.dist
    station = interpolate(reference.lap.channels.t, referenceStations, from)
  }
  const stations = entry.stations || entry.lap.channels.dist
  return interpolate(stations, entry.lap.channels.t, station)
}

// preparePlaybackStarts lines every lap up on the stretch's entry, so playback
// releases them together and they separate exactly as they did on track.
function preparePlaybackStarts() {
  const from = state.selection ? state.selection.from : 0
  for (const entry of loadedEntries()) {
    entry.playbackStart = stretchEntryTime(entry, from)
  }
}

// lapCursorValue is where one lap is right now. Scrubbing compares every lap at
// the same point on track; playback races them from a common start instead.
function lapCursorValue(entry) {
  const playback = state.playback
  if (!playback.engaged || entry.playbackStart == null) return state.cursorX
  const time = entry.playbackStart + playback.elapsed
  if (state.axis === 'time') return time
  return interpolate(entry.lap.channels.t, entry.lap.channels.dist, time)
}

function playbackBounds() {
  const full = axisMax()
  const from = state.selection ? state.selection.from : 0
  const to = state.selection ? state.selection.to : full
  const startTime = axisToTime(from)
  const endTime = axisToTime(to)
  if (startTime == null || endTime == null || endTime <= startTime) return null
  return { startTime, endTime }
}

function togglePlayback() {
  if (state.playback.playing) stopPlayback()
  else startPlayback()
  updatePlaybackControls()
}

function startPlayback() {
  const bounds = playbackBounds()
  if (!bounds) return
  const playback = state.playback
  playback.playing = true
  // Resume where it was paused, unless that is outside the stretch.
  if (playback.time < bounds.startTime || playback.time >= bounds.endTime) {
    playback.time = bounds.startTime
  }
  preparePlaybackStarts()
  playback.engaged = true
  playback.elapsed = playback.time - bounds.startTime
  let previous = performance.now()
  const step = (now) => {
    if (!playback.playing) return
    const current = playbackBounds()
    if (!current) { stopPlayback(); updatePlaybackControls(); return }
    playback.time += ((now - previous) / 1000) * playback.speed
    previous = now
    if (playback.time >= current.endTime) {
      if (playback.loop) playback.time = current.startTime
      else {
        playback.time = current.endTime
        playback.playing = false
        updatePlaybackControls()
      }
    }
    // Elapsed since the stretch opened, which is what every lap is driven from.
    playback.elapsed = playback.time - current.startTime
    const value = timeToAxis(playback.time)
    if (value != null) {
      // Playback owns the cursor the same way a pin does: hover must not fight it.
      state.cursorX = value
      followCursorOnMap()
      scheduleRedraw()
    }
    if (playback.playing) playback.handle = requestAnimationFrame(step)
  }
  playback.handle = requestAnimationFrame(step)
}

function stopPlayback() {
  const playback = state.playback
  playback.playing = false
  if (playback.handle) cancelAnimationFrame(playback.handle)
  playback.handle = null
}

function cursorLocked() {
  return state.cursorPinned || state.playback.playing
}

function updatePlaybackControls() {
  const bar = el('playbackBar')
  const hasLaps = loadedEntries().length > 0
  bar.hidden = !hasLaps
  el('playToggle').textContent = state.playback.playing ? '⏸' : '▶'
  el('playToggle').title = state.playback.playing ? t('pause') : t('play')
  el('loopToggle').classList.toggle('active', state.playback.loop)
  el('loopToggle').title = t('loop')
  el('playSpeed').value = String(state.playback.speed)

  const label = el('selectionLabel')
  if (!state.selection) {
    label.textContent = t('selectionNone')
    label.classList.remove('has-selection')
    el('clearSelection').hidden = true
    return
  }
  const unit = state.axis === 'dist' ? 'm' : 's'
  const digits = state.axis === 'dist' ? 0 : 2
  label.textContent =
    `${state.selection.from.toFixed(digits)}–${state.selection.to.toFixed(digits)} ${unit}`
  label.classList.add('has-selection')
  el('clearSelection').hidden = false
}

/* ------------------------------------------------------------------ zoom */

function clamp(value, low, high) {
  return value < low ? low : value > high ? high : value
}

// attachMapZoom: wheel zooms about the pointer, drag pans, double-click resets.
function attachMapZoom() {
  const canvas = el('map')
  canvas.addEventListener('wheel', (event) => {
    event.preventDefault()
    const rect = canvas.getBoundingClientRect()
    const pointerX = event.clientX - rect.left
    const pointerY = event.clientY - rect.top
    const view = state.mapView
    const next = clamp(view.scale * Math.exp(-event.deltaY * 0.0015), 1, 40)
    const projection = state.mapProjection
    if (projection && projection.headingUp) {
      // Zoom around the car, which stays put: panning to the pointer would slide
      // it off its mark and the view would stop reading as navigation.
      view.scale = next
      updateZoomControls()
      scheduleRedraw()
      return
    }
    const centreX = projection ? projection.centreX : rect.width / 2
    const centreY = projection ? projection.centreY : rect.height / 2
    // Keep the world point under the pointer pinned to the pointer.
    const baseX = (pointerX - centreX - view.panX) / view.scale + centreX
    const baseY = (pointerY - centreY - view.panY) / view.scale + centreY
    view.panX = pointerX - centreX - (baseX - centreX) * next
    view.panY = pointerY - centreY - (baseY - centreY) * next
    view.scale = next
    if (next <= 1.001) {
      view.scale = 1
      view.panX = 0
      view.panY = 0
    }
    updateZoomControls()
    scheduleRedraw()
  }, { passive: false })

  canvas.addEventListener('contextmenu', (event) => event.preventDefault())
  canvas.addEventListener('mousedown', (event) => {
    state.dragMoved = false
    if (event.button === 2 || (event.button === 0 && event.ctrlKey)) {
      event.preventDefault()
      const rect = canvas.getBoundingClientRect()
      state.selecting = {
        pane: 'map',
        x0: event.clientX - rect.left,
        y0: event.clientY - rect.top,
        x1: event.clientX - rect.left,
        y1: event.clientY - rect.top
      }
      scheduleRedraw()
      return
    }
    // Heading up, the map is anchored to the car; dragging it would only move
    // the car off the spot the mode exists to keep it on.
    if (event.button !== 0 || state.mapView.scale <= 1.001 ||
      (state.mapProjection && state.mapProjection.headingUp)) return
    event.preventDefault()
    state.mapDrag = {
      x: event.clientX,
      y: event.clientY,
      panX: state.mapView.panX,
      panY: state.mapView.panY
    }
    canvas.classList.add('grabbing')
  })
  canvas.addEventListener('dblclick', resetMapView)
  el('mapReset').addEventListener('click', resetMapView)
  el('mapOrient').addEventListener('click', () =>
    setMapOrientation(state.mapOrientation === 'heading' ? 'north' : 'heading'))
  el('mapTilt').addEventListener('click', () => setMapTilt(!state.mapTilt))
  el('mapRoads').addEventListener('click', () => setShowRoads(!state.showRoads))
}

// attachChartZoom: the charts share one X window, so zooming any of them zooms
// all of them and the readout stays aligned with the map cursor.
function attachChartZoom(canvas) {
  canvas.addEventListener('wheel', (event) => {
    const chart = state.charts.find((item) => item.canvas === canvas)
    if (!chart || !chart.geometry) return
    event.preventDefault()
    const rect = canvas.getBoundingClientRect()
    const { left, plotWidth, from, span } = chart.geometry
    const ratio = clamp((event.clientX - rect.left - left) / plotWidth, 0, 1)
    const anchor = from + ratio * span
    const full = axisMax()
    const nextSpan = clamp(span * Math.exp(event.deltaY * 0.0015), full / 500, full)
    const nextFrom = clamp(anchor - ratio * nextSpan, 0, full - nextSpan)
    state.xRange = nextSpan >= full ? null : { from: nextFrom, to: nextFrom + nextSpan }
    updateZoomControls()
    scheduleRedraw()
  }, { passive: false })

  canvas.addEventListener('contextmenu', (event) => event.preventDefault())
  canvas.addEventListener('mousedown', (event) => {
    state.dragMoved = false
    if (event.button === 2 || (event.button === 0 && event.ctrlKey)) {
      const value = chartValueAt(canvas, event.clientX)
      if (value == null) return
      event.preventDefault()
      state.selecting = { pane: 'chart', from: value, to: value }
      scheduleRedraw()
      return
    }
    if (event.button !== 0 || !isZoomed()) return
    const chart = state.charts.find((item) => item.canvas === canvas)
    if (!chart || !chart.geometry) return
    event.preventDefault()
    state.chartDrag = {
      x: event.clientX,
      span: chart.geometry.span,
      plotWidth: chart.geometry.plotWidth,
      from: chart.geometry.from
    }
  })
  canvas.addEventListener('dblclick', resetRange)
}

// One document-level pair of handlers drives both drags: a gesture that starts
// on a canvas must keep working after the pointer leaves it.
function attachDragging() {
  document.addEventListener('mousemove', (event) => {
    if (state.selecting) {
      state.dragMoved = true
      if (state.selecting.pane === 'map') {
        const rect = el('map').getBoundingClientRect()
        state.selecting.x1 = event.clientX - rect.left
        state.selecting.y1 = event.clientY - rect.top
      } else {
        const canvas = state.charts[0] && state.charts[0].canvas
        const chart = state.charts.find((item) => item.geometry)
        if (chart) {
          const rect = chart.canvas.getBoundingClientRect()
          const { left, plotWidth, from, span } = chart.geometry
          const ratio = clamp((event.clientX - rect.left - left) / plotWidth, 0, 1)
          state.selecting.to = from + ratio * span
        }
        void canvas
      }
      scheduleRedraw()
      return
    }
    if (state.mapDrag || state.chartDrag) {
      const drag = state.mapDrag || state.chartDrag
      if (Math.abs(event.clientX - drag.x) > 3 || Math.abs(event.clientY - (drag.y ?? event.clientY)) > 3) {
        state.dragMoved = true
      }
    }
    if (state.mapDrag) {
      state.mapView.panX = state.mapDrag.panX + (event.clientX - state.mapDrag.x)
      state.mapView.panY = state.mapDrag.panY + (event.clientY - state.mapDrag.y)
      scheduleRedraw()
      return
    }
    if (state.chartDrag) {
      const drag = state.chartDrag
      const full = axisMax()
      const moved = ((event.clientX - drag.x) / drag.plotWidth) * drag.span
      const from = clamp(drag.from - moved, 0, full - drag.span)
      state.xRange = { from, to: from + drag.span }
      updateZoomControls()
      scheduleRedraw()
    }
  })
  document.addEventListener('mouseup', () => {
    if (state.selecting) {
      const gesture = state.selecting
      state.selecting = null
      if (gesture.pane === 'map') {
        selectionOnMap({
          left: Math.min(gesture.x0, gesture.x1),
          right: Math.max(gesture.x0, gesture.x1),
          top: Math.min(gesture.y0, gesture.y1),
          bottom: Math.max(gesture.y0, gesture.y1)
        })
      } else {
        setSelection(gesture.from, gesture.to)
      }
    }
    state.mapDrag = null
    state.chartDrag = null
    el('map').classList.remove('grabbing')
  })
}

// setCursor moves the shared cursor.
//
// `source` says which pane drove it, so a cursor moved from the charts or the
// sector table can drag the zoomed map along with it. `pin` marks the move as
// deliberate: hovering is transient and is ignored while a pin is held, which
// is what lets the reader park a position on one pane and go work on the other.
function setCursor(value, options = {}) {
  const { source, pin } = options
  if (cursorLocked() && !pin) return
  // Moving the cursor by hand is a return to comparing every lap at one point.
  if (source) disengagePlayback()
  if (pin) state.cursorPinned = value != null
  if (state.cursorX === value) {
    if (pin) scheduleRedraw()
    return
  }
  state.cursorX = value
  if (source !== 'map') followCursorOnMap()
  scheduleRedraw()
}

function releaseCursor() {
  if (!state.cursorPinned) return
  state.cursorPinned = false
  state.cursorX = null
  scheduleRedraw()
}

// centreOnCursor brings a pinned point back into a zoomed map, for when the
// reader has panned away from it.
function centreOnCursor() {
  const projection = state.mapProjection
  if (!projection || state.cursorX == null || state.mapView.scale <= 1.001) return
  const anchor = cursorAnchor()
  if (!anchor) return
  state.mapView.panX += projection.width / 2 - anchor.x
  state.mapView.panY += projection.height / 2 - anchor.y
  scheduleRedraw()
}

// cursorAnchor is the projected point the map should keep in view: the
// reference lap's position at the cursor, or the first lap that still has data
// there when the reference has already ended.
function cursorAnchor() {
  if (!state.mapProjected) return null
  const reference = referenceLap()
  const ordered = state.mapProjected.slice().sort((a, b) => {
    if (!reference) return 0
    return (lapKey(b.entry) === lapKey(reference)) - (lapKey(a.entry) === lapKey(reference))
  })
  for (const projected of ordered) {
    const values = axisValues(projected.entry.lap)
    if (!values.length || values[values.length - 1] < state.cursorX) continue
    const index = indexAt(values, state.cursorX)
    if (index < 0) continue
    return { x: projected.points[index * 2], y: projected.points[index * 2 + 1] }
  }
  return null
}

// followCursorOnMap recentres the zoomed map when the cursor leaves the middle
// of the viewport. Recentring on every step would make the map crawl under the
// reader; leaving it alone until the point nears an edge keeps it still for
// most of a scrub and never loses the point.
function followCursorOnMap() {
  const view = state.mapView
  const projection = state.mapProjection
  if (view.scale <= 1.001 || state.cursorX == null || state.mapDrag || !projection) return
  // Heading up keeps the cursor at the anchor by construction.
  if (state.mapOrientation === 'heading') return

  const anchor = cursorAnchor()
  if (!anchor) return
  const { width, height } = projection
  const marginX = width * 0.2
  const marginY = height * 0.2

  // Pan by the least that brings the point back inside the safe box. Scrubbing
  // then slides the map smoothly along with the cursor instead of snapping it
  // to the centre on every step.
  let panX = 0
  let panY = 0
  if (anchor.x < marginX) panX = marginX - anchor.x
  else if (anchor.x > width - marginX) panX = width - marginX - anchor.x
  if (anchor.y < marginY) panY = marginY - anchor.y
  else if (anchor.y > height - marginY) panY = height - marginY - anchor.y
  if (panX === 0 && panY === 0) return

  // A jump of more than a screen is not a scrub — it is the cursor landing
  // somewhere else entirely (a sector row, say). Centre on it instead of
  // dragging it in from off screen.
  if (Math.abs(panX) > width * 0.75 || Math.abs(panY) > height * 0.75) {
    panX = width / 2 - anchor.x
    panY = height / 2 - anchor.y
  }

  view.panX += panX
  view.panY += panY
}

let redrawHandle = null
function scheduleRedraw() {
  if (redrawHandle) return
  redrawHandle = requestAnimationFrame(() => {
    redrawHandle = null
    drawMap()
    drawCharts()
    renderReadout()
  })
}

function renderReadout() {
  const readout = el('readout')
  const entries = loadedEntries()
  if (!entries.length) {
    readout.textContent = t('readoutHint')
    return
  }
  if (state.cursorX == null) {
    const reference = referenceLap()
    readout.textContent = t('readoutSummary', entries.length,
      reference && reference.lap ? (reference.lap.label || reference.id) : '—')
    return
  }

  readout.textContent = ''
  if (state.cursorPinned) {
    const badge = document.createElement('span')
    badge.className = 'pin-badge'
    badge.title = t('pinCentre')
    const label = document.createElement('span')
    label.textContent = `📌 ${t('pinned')}`
    label.addEventListener('click', centreOnCursor)
    const release = document.createElement('span')
    release.className = 'release'
    release.textContent = '✕'
    release.title = t('pinRelease')
    release.addEventListener('click', releaseCursor)
    badge.append(label, release)
    readout.append(badge)
  }
  const head = document.createElement('span')
  head.innerHTML = state.axis === 'dist'
    ? `${t('readoutPosition')} <b>${state.cursorX.toFixed(0)} m</b>`
    : `${t('readoutTime')} <b>${state.cursorX.toFixed(2)} s</b>`
  readout.append(head)

  for (const entry of entries) {
    const channels = entry.lap.channels
    const values = axisValues(entry.lap)
    const own = lapCursorValue(entry)
    const index = indexAt(values, own)
    if (index < 0) continue
    const beyond = values[values.length - 1] < own
    const span = document.createElement('span')
    const parts = [`<b style="color:${entry.color}">■</b>`]
    if (beyond) {
      parts.push(`<span style="opacity:.5">${t('readoutEnded')}</span>`)
    } else {
      parts.push(`<b>${kmh(channels.speed[index]).toFixed(1)}</b>km/h`)
      if (entry.lap.hasInputs) {
        parts.push(`${t('readoutThrottle')}<b>${(channels.throttle[index] * 100).toFixed(0)}</b>`)
        parts.push(`${t('readoutBrake')}<b>${(channels.brake[index] * 100).toFixed(0)}</b>`)
        parts.push(`${t('readoutGear')}<b>${channels.gear[index].toFixed(0)}</b>`)
      }
      parts.push(`${channels.latG[index].toFixed(2)}G`)
      if (channels.gradient) parts.push(`${channels.gradient[index].toFixed(1)}%`)
      if (entry.delta) {
        const delta = entry.delta[index]
        parts.push(`<b class="${delta >= 0 ? 'pos' : 'neg'}">${formatDelta(delta)}</b>`)
      }
    }
    span.innerHTML = parts.join(' ')
    readout.append(span)
  }
}

/* -------------------------------------------------------------- deep link */

// The hash carries the current view so a specific comparison can be linked or
// reloaded: #lib=<key>&laps=1,3&axis=dist&color=speed&ref=3
function writeHash() {
  if (!state.activeLibrary) return
  const laps = state.selected.map((entry) => entry.id)
  const parts = [`lib=${encodeURIComponent(state.activeLibrary.key)}`]
  if (laps.length) parts.push(`laps=${laps.join(',')}`)
  const reference = referenceLap()
  if (reference) parts.push(`ref=${reference.id}`)
  parts.push(`axis=${state.axis}`, `color=${state.colorMode}`)
  const hash = `#${parts.join('&')}`
  if (location.hash !== hash) history.replaceState(null, '', hash)
}

function readHash() {
  const raw = location.hash.replace(/^#/, '')
  if (!raw) return null
  const params = new URLSearchParams(raw)
  const lib = params.get('lib')
  if (!lib) return null
  return {
    lib,
    laps: (params.get('laps') || '').split(',').filter(Boolean),
    reference: params.get('ref'),
    axis: params.get('axis'),
    color: params.get('color')
  }
}

async function applyHash(request) {
  const library = state.libraries.find((item) => item.key === request.lib)
  if (!library) return false
  if (request.axis === 'time' || request.axis === 'dist') {
    state.axis = request.axis
    for (const node of el('axisMode').children) node.classList.toggle('active', node.dataset.axis === state.axis)
  }
  if (request.color) {
    state.colorMode = request.color
    el('colorMode').value = request.color
  }
  await openLibrary(library, request.laps.length > 0)
  if (request.laps.length) {
    state.selected = []
    state.referenceKey = null
    for (const id of request.laps) {
      const lap = state.laps.find((item) => item.id === id)
      if (lap) await toggleLap(lap)
    }
    if (request.reference) {
      const match = state.selected.find((entry) => entry.id === request.reference)
      if (match) state.referenceKey = lapKey(match)
    }
    render()
  }
  return true
}

/* ------------------------------------------------------------------ boot */

function render() {
  refreshDeltas()
  computeSectors()
  for (const entry of state.selected) {
    if (entry.cache) delete entry.cache.delta
  }
  renderChips()
  renderLapList()
  buildCharts()
  renderSummary()
  updatePlaybackControls()
  ensureRoads()
  writeHash()
  scheduleRedraw()
}

function wire() {
  state.collapsed = loadTreeState()
  state.panels = loadPanels()
  state.mapOrientation = readSetting(ORIENTATION_STORAGE, 'north') === 'heading' ? 'heading' : 'north'
  state.mapTilt = readSetting(TILT_STORAGE, '0') === '1'
  state.showRoads = readSetting(ROADS_STORAGE, '0') === '1'
  state.lang = detectLanguage()
  state.theme = readSetting(THEME_STORAGE, 'auto')
  applyTheme()
  applyStaticText()

  el('langMode').addEventListener('click', (event) => {
    const chip = event.target.closest('.chip')
    if (chip) setLanguage(chip.dataset.lang)
  })

  el('themeMode').addEventListener('click', (event) => {
    const chip = event.target.closest('.chip')
    if (chip) setTheme(chip.dataset.themeMode)
  })

  // Following the system theme means reacting when the system changes.
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (state.theme === 'auto') {
      themeColorCache = null
      assignLapColors()
      render()
    }
  })

  el('rescan').addEventListener('click', async () => {
    try {
      await loadCatalog(true)
      toast(t('rescanned'))
    } catch (error) {
      toast(error.message, true)
    }
  })

  el('search').addEventListener('input', (event) => {
    state.filters.search = event.target.value.trim()
    renderTree()
  })

  el('categoryFilter').addEventListener('click', (event) => {
    const chip = event.target.closest('.chip')
    if (!chip) return
    state.filters.category = chip.dataset.category
    for (const node of el('categoryFilter').children) node.classList.toggle('active', node === chip)
    renderLapList()
  })

  el('axisMode').addEventListener('click', (event) => {
    const chip = event.target.closest('.chip')
    if (!chip) return
    state.axis = chip.dataset.axis
    for (const node of el('axisMode').children) node.classList.toggle('active', node === chip)
    // The window and the selection are in the old axis's units; metres do not
    // carry over to seconds.
    state.xRange = null
    state.selection = null
    stopPlayback()
    state.cursorX = null
    updateZoomControls()
    updatePlaybackControls()
    scheduleRedraw()
  })

  el('vehicleFilter').addEventListener('change', (event) => {
    state.filters.vehicle = event.target.value
    renderLapList()
  })

  el('sortMode').addEventListener('change', (event) => {
    state.filters.sort = event.target.value
    renderLapList()
  })

  el('lapPanelToggle').addEventListener('click', () => togglePanel('laps'))
  el('summaryToggle').addEventListener('click', () => togglePanel('summary'))

  el('summaryTab').addEventListener('click', (event) => {
    const chip = event.target.closest('.chip')
    if (!chip) return
    state.summaryTab = chip.dataset.tab
    renderSummary()
  })

  el('colorMode').addEventListener('change', (event) => {
    state.colorMode = event.target.value
    scheduleRedraw()
  })

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      stopPlayback()
      updatePlaybackControls()
      releaseCursor()
    }
    // Space is the usual transport key, but not while typing in the search box.
    if (event.key === ' ' && event.target === document.body) {
      event.preventDefault()
      togglePlayback()
    }
  })

  attachMapCursor()
  attachMapZoom()
  attachDragging()
  el('rangeReset').addEventListener('click', resetRange)
  el('playToggle').addEventListener('click', togglePlayback)
  el('loopToggle').addEventListener('click', () => {
    state.playback.loop = !state.playback.loop
    updatePlaybackControls()
  })
  el('playSpeed').addEventListener('change', (event) => {
    state.playback.speed = Number(event.target.value) || 1
  })
  el('clearSelection').addEventListener('click', clearSelection)
  window.addEventListener('resize', scheduleRedraw)
}

async function boot() {
  wire()
  try {
    await loadCatalog(false)
    const request = readHash()
    if (request && await applyHash(request)) return
    const first = state.libraries.find((library) => library.lapCount > 0)
    if (first) await openLibrary(first)
    else render()
  } catch (error) {
    toast(error.message, true)
  }
}

boot()
