# Ghost Racer 遥测 Web

[English](README.en.md) · 简体中文

把 **Ghost Racer Enhanced**（BeamNG.drive 圈速 Ghost Mod，独立项目）记录的圈速遥测读出来，在浏览器里查看轨迹、通道曲线和多圈 Δt 对比。

单个 Go 二进制，零依赖，默认只监听 `127.0.0.1`。两种取数方式，可以同时用：

1. **直接解析存档目录**（默认）：启动时扫描 BeamNG 用户目录下的 `ghostReplays/`，游戏里跑完点一下"重新扫描"就能看到新圈。不需要改 mod。
2. **游戏内推送**（端点已就绪）：mod 里的"导出分析"按钮把选中的记录 POST 到 `/api/import`，服务写进自己的数据目录，和存档库并列显示。协议见 [`docs/import-api.md`](docs/import-api.md)。

## 跑起来

从 [Releases](https://github.com/flintt/ghost-racer-telemetry/releases) 下对应平台的单文件二进制，或者自己编译：

```bash
go build -o ghost-racer-telemetry .   # Windows: GOOS=windows GOARCH=amd64 go build -o ghost-racer-telemetry.exe .
./ghost-racer-telemetry
```

不带参数时会自己找 BeamNG 用户目录，然后打开 <http://127.0.0.1:8777/>。Windows 下依次试这些位置的 `ghostReplays`：

```text
%LOCALAPPDATA%\BeamNG\BeamNG.drive\current\      ← 现在的安装是这个（current 是指向当前版本的联接）
%LOCALAPPDATA%\BeamNG\BeamNG.drive\<版本号>\      ← 没有 current 就挑版本号最大的
%LOCALAPPDATA%\BeamNG.drive\<版本号>\              ← 旧版安装
```

找不到或者想指定时：

```bash
./ghost-racer-telemetry -root "C:\Users\你\AppData\Local\BeamNG\BeamNG.drive\current"
```

`-root` 给用户目录、给版本目录、或者直接给 `ghostReplays` 目录都行。找不到时会把试过的路径全打出来。

> **别把游戏目录填给 `-data`。** `-root` 是**只读**的游戏存档，`-data` 是服务自己的**可写**导入目录——填错了记录照样能看，但那棵树会被当成服务自己的数据，删除不再受 `-allow-delete` 保护，点一下就真删了游戏里的记录。填错时启动会有警告。

### 参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-addr` | `127.0.0.1:8777` | 监听地址。留在回环地址上，除非确实要让别的机器访问 |
| `-root` | 自动探测 | BeamNG 用户目录或其中的 `ghostReplays` 目录，**只读** |
| `-data` | 用户配置目录下 `bng_ghost_web/ghostReplays` | 游戏内导出的记录写到这里，可写 |
| `-web` | 内嵌 | 从磁盘目录提供前端，改前端时不用重新编译 |
| `-allow-delete` | `false` | 允许删除**游戏存档里**的记录。删之前先关掉 BeamNG，否则它会把内存里的清单再写回去 |
| `-token` | 空 | 给 `/api/import` 加一个共享口令（`X-Ghost-Token` 头或 `?token=`） |

## 界面

- **左栏**：按 `地图 → 起点 → 赛道变体` 分层列出，和游戏里的层级一致，可逐层折叠，展开状态记在浏览器里。地图超过 6 个时默认收起（74 个库全展开没法看），当前打开的那条会自动展开并滚到可见处；单一变体的起点直接显示为叶子，不多套一层。`P2P` 是点对点，`TT` 是 Time Trial，`导入` 是游戏内推上来的。搜索时全部展开。
- **记录列表**：按圈速/名次/录制顺序排序，可按完整圈 / 未完成 / 手动录制和车型筛选。名次只算有计时的完整圈，和 mod 里的分类口径一致。每行可以导出 CSV 或删除。
- **轨迹图**：俯视轨迹，着色可切换速度、油门/刹车、档位、横向 G、Δt 对比或按记录配色。起点门画成绿色虚线，终点门（点对点）画成黄色。
- **曲线区**：速度、Δt、油门/刹车、横向 G、纵向加速度、档位。鼠标在轨迹图或任意曲线上移动，所有图共用一个游标，顶部读数栏同步显示每条记录在该位置的数值。
- **汇总表**：圈速、距离、最高/平均/最低速、最大加减速、最大横向 G、全油门/刹车/滑行占比、爬升、采样点数、车型。
- **界面语言**中英文可切（首次按浏览器语言自动选），**主题**有自动 / 浅色 / 深色三档（自动跟随系统），选择记在浏览器里。

### 放大

两边独立缩放，互不干扰：

- **轨迹图**：滚轮以指针为中心缩放（最大 40×），放大后拖动平移，双击或右上角 **1:1** 还原。缩放会钉住指针下的那个点，所以对准一个弯滚就是放大那个弯。
- **曲线区**：滚轮缩放 X 轴窗口（最小到全程的 1/500），拖动平移，双击还原；六张图共用同一个窗口，游标和读数跟着走。**Y 轴只按窗口内的数据定标**——放大一个弯时，速度曲线会真的把那段展开，而不是贴在整圈的量程上压成一条平线。顶栏会出现 `全程 · 900–1500 m` 的复位按钮。

切换距离/时间轴会清掉窗口（米的范围换算不到秒上）。放大时轨迹图会剔除视口外的线段，所以越放大画得越快。

### 分段分析（最佳表现区间）

选中两条以上记录时，「分段」页会把共同路线切成 10 米一格，逐格比谁用时最短，那一格就归谁。连续归属同一条记录的格子合成一个**区间**，就是这条记录的最佳表现段。

- **理论最佳圈**：把每格的最快用时加起来。和实际最快圈的差，就是"同样的开法还能快多少"。
- **区间表**：`起止距离 / 归属 / 段用时 / 领先次快 / 相对参照圈`，按领先幅度排序——排在最上面的就是最该去练的那一段。鼠标划过表格行，地图和所有曲线的游标会跳到那一段中点。
- **轨迹着色**选「分段归属」：赢下的段用该记录的颜色，其余画灰。Δt 图顶部也有一条同样的归属带。

三个实现上的取舍：

1. **按参照圈的路径取里程，不按各自跑过的距离。** 用自己的里程会系统性地冤枉走外线的圈——跑了 500 米时它其实还没到参照圈的 500 米处，却被拿去和那里比。实测一条外扩 3 米的线自己跑了 2306.3 米，投影到参照路径上是 2289.7 米，正好等于参照圈长度，这 16.6 米的偏差就是不做投影时的误差。
2. **比的是每格净耗时，不是累计时间。** 累计时间（Δt 曲线本身）会把上游的优势一路背下去，看着像整圈都在赢；只有格内耗时才回答"这一段单独看谁快"。Δt 曲线的斜率是等价的信息。
3. **噪声要清掉。** 夹在两段同主之间、短于 3 格的归属判为抖动，并入邻段；领先幅度不到 0.02 秒的区间不进表。

未完成片段按它实际跑到的地方参与：只在覆盖到的格子里竞争，不会把整个对比范围截短，也不参与理论最佳圈的总时对比。同一起点下不同车型的记录会一起参与归属，需要时用车型筛选分开看。

**X 轴可以切距离或时间。** 默认是距离——多圈对比按"从起点跑了多远"对齐才有意义，Δt 也是这么算的：把参照圈的时间插值到当前圈每个采样点所在的距离上，两者相减。所以未完成的片段会在它停下的地方结束，不会污染对比。

地址栏的 hash 记录了当前视图（`#lib=…&laps=1,3&ref=1&axis=dist&color=speed`），可以直接存成书签或发给别人。

## 数据来源

服务读的是 mod 写在用户目录下的这些文件（只读，除非开了 `-allow-delete`）：

```text
ghostReplays/freeRoam/<level>/startLines.json                              起点注册表（名字、位置、终点门、变体分组 startKey）
ghostReplays/freeRoam/<level>/starts/<startId>/ghostracer.save.library.json  圈清单
ghostReplays/freeRoam/<level>/starts/<startId>/ghostracer.save.ghosts/<id>.json  样本
ghostReplays/races/<level>/<raceKey>/ghostracer.save.*                     Time Trial / 比赛
ghostReplays/freeRoam/<level>/<vehicleDir>/...                             2.9.8 之前的旧布局，同样能读
```

样本是紧凑数组，下标固定：

```text
1=t  2..4=位置 xyz  5..7=前向量  8..10=上向量  11=速度(m/s)
12=油门  13=刹车  14=档位  15=手刹  16=离合          ← 2.18 起才有
```

原版 1.6 的对象格式（`{pos, dirFront, dirUp, speed}`，没有时间戳）也能读，时间戳按 `sampleInterval` 推算。横向 G 和纵向加速度是服务端从位置/速度/前向量算出来的，mod 本身不记录这两个通道。

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/catalog` | 缓存的记录库目录 |
| `POST` | `/api/rescan` | 重新扫描磁盘 |
| `GET` | `/api/laps?lib=<key>` | 一个库里的所有圈（元数据） |
| `GET` | `/api/lap?lib=<key>&id=<id>` | 单圈的完整通道和汇总 |
| `DELETE` | `/api/lap?lib=<key>&id=<id>` | 删除一圈（受 `-allow-delete` 约束） |
| `POST` | `/api/import` | 接收游戏内导出的记录，见 [`docs/import-api.md`](docs/import-api.md) |

`lib` 的 key 形如 `game:freeRoam/east_coast_usa/starts/s001/ghostracer.save.json`，前缀是数据源（`game` / `import`）。

## 开发

```bash
./tools/check.sh                                        # gofmt + vet + test + 前端语法 + UI 冒烟 + 双平台编译
./tools/build.sh v0.1.0                                 # 打 release 产物到 dist/（4 个平台 + SHA256SUMS）
./tools/smoke.sh                                        # 单跑冒烟：无头 Chrome 加载真实页面，有 JS 报错就失败
go run ./cmd/genfixture -out /tmp/gr/ghostReplays        # 造一份假的存档树
go run ./cmd/genfixture -out /tmp/gr/ghostReplays -size 6  # 加大圈长（约 9500 采样点/圈）用来压渲染
go run . -root /tmp/gr/ghostReplays -web ./web          # 前端改完刷新即可，不用重编译
```

### 渲染

轨迹和曲线分成**静态层**和**游标层**：轨迹、网格、坐标和曲线画进离屏 canvas，键由"选了哪些圈 / 尺寸 / 着色模式 / X 轴"决定；鼠标移动只做一次 `drawImage` 加十字线和圆点。轨迹按屏幕像素抽稀并把颜色量化成 32 档，同色连成一条 path 一次描边；曲线在采样点多于像素列时按列取 min/max，既不丢刹车尖峰也不按行遍历。

实测（无头 Chrome 软件渲染，5 圈 / 45909 采样点）：游标重绘 **0.60 ms**，同样内容不走缓存是 **191 ms**。

## 许可证

MIT，见 [`LICENSE`](LICENSE)。产生这些记录的 BeamNG.drive Mod 是独立项目，用它自己的许可证。
