# Stoa 🏛️

[English](README.md)

> 讓 AI 代理不只會推理，也能在驗證後行動。

**Stoa** 是一個用 Go 打磨可上線 AI 代理的工作坊。它不是框架，而是一種設計與實作代理的思考方式。

它建立在兩個信念上：

1. **代理是知道與做到的結合。** 只會推理的大型語言模型只是聊天機器人。代理會行動，而行動必須可以被驗證。

2. **控制你能控制的，讓模型保持模型。** 架構、合約、驗證、錯誤處理，是我們可以掌握的工程面。模型的機率性不是敵人，而是需要被駕馭的能力。

---

## 理念

Stoa 這個名字來自希臘文 στοά，意思是市集旁的有頂廊柱。**季蒂昂的芝諾**曾在那裡講學，後來形成了**斯多葛學派**。那是一個思考與行動交會的地方。

兩個相隔兩千年的傳統，走向相近的原則：

- **斯多葛學派**：控制二分法，專注於你能控制的事，接受你不能控制的事。
- **王陽明**：*知行合一*，不能落實於行動的知，不是真正的知。

這兩者都很適合描述今天建構 AI 代理的工程實踐。

### 核心信念

**領域模型是代理的良知。**
它把通用能力收束成具體業務判斷。好的領域模型不只告訴模型要產生什麼，也定義在這個世界裡什麼才算有效。

**駕馭工程是在真實工作上練出來的。**
穩定性不是靠更聰明的提示詞，而是靠真實任務的磨合。我們不相信提示詞能保證業務規則；我們相信驗證器、型別、明確合約與回饋迴路。

**每個代理都應該知行合一。**
不只是產生文字，而是可驗證的行動。如果代理不能行動，它還不是代理。如果行動不能被驗證，工作還沒有完成。

**我們控制能控制的。**
架構、合約、驗證、錯誤處理，這些是確定性的，也是我們能掌握的。模型的機率性應該被約束、導引、駕馭，而不是被否認。

---

## 為什麼需要 Stoa

多數 AI 代理工具大致落在兩端：

- **厚重框架**，例如 LangChain：它們為了通用性做抽象，但當你知道自己要什麼時，這些抽象可能變成負擔。
- **裸 SDK 腳本**：很快可以開始，但缺少結構時，很容易在上線需求下崩壞，例如錯誤處理、多代理協作、可觀測性。

Stoa 站在中間。它不是一個要你採用的框架，而是一套你可以遵循的架構。一組可以在一個下午讀完、一個晚上修改、而且能完整理解的模式、合約與駕馭工程元件。

目標不是隱藏複雜度，而是**把複雜度放在它該在的地方**：業務規則在領域模型，流程編排在使用案例，模型與供應商細節在轉接器。

---

## 設計原則

- 🏛️ **知行合一。** 沒有經過驗證行動的推理是不完整的。
- 🔑 **領域模型先於提示詞。** 不要讓模型能力反過來決定你的業務模型。
- 🔑 **提示詞承載判斷，程式碼承載合約。** 如果某條規則可以寫成驗證器，就不應該只存在提示詞裡。
- 🔑 **大型語言模型是基礎設施，不是領域模型。** 它在外層，你的業務邏輯不應該匯入 SDK。
- 🔑 **合約是結構化的，不是自由文字。** 代理透過具型別的交接物件溝通。
- 🔑 **先窄後深，勝過又廣又淺。** 先選一個領域，把它做深，再談泛化。
- 🔑 **需要時才打開轉接器。** 不要為假想需求過早設計。
- 🔑 **在工作本身上練習。** 真實任務會揭露純思考實驗看不到的問題。

---

## 架構

Stoa 採用**整潔架構**，依賴方向往內。大型語言模型、框架與外部服務都在外層；業務邏輯不需要知道它們存在。

```text
基礎設施
  模型供應商、資料庫、外部服務
  ↓
介面轉接器
  模型轉接器、提示詞模板、解析器
  ↓
使用案例
  代理任務流程、流程編排
  ↓
領域
  純粹的業務模型與規則
```

依賴方向往內：外層可以依賴內層，內層不應該知道外層存在。

| 層級 | 職責 | 例子 |
|-------|---------------|----------|
| **領域層** | 純粹的實體、規則、驗證器 | 業務實體與不變條件 |
| **使用案例層** | 代理任務流程、決策邏輯 | 流程編排 |
| **轉接器層** | 在領域與基礎設施之間翻譯 | 模型轉接器、提示詞模板 |
| **基礎設施層** | 具體 SDK、資料庫、外部工具 | 模型供應商 SDK、PostgreSQL |

完整說明請看 [`docs/architecture.md`](docs/architecture.md)。

---

## 主要方向：遊戲 NPC 駕馭工程

> **Stoa：以領域驗證駕馭 LLM 驅動的遊戲 NPC**

Stoa 目前的主要方向，是證明一個由大型語言模型驅動的 NPC，可以：提出帶型別的意圖、由遊戲領域的硬規則驗證、通過驗證後才執行、並在收到結構化回饋後自我修正——而且整條流程不會讓遊戲邏輯滲入 LLM 層。

```text
世界情境
→ LLM 提出 NPCIntent（say、emotion、action）
→ world.Validator 執行遊戲規則
→ executor 觀察/變更世界狀態
→ 驗證錯誤以具型別事件回饋給下一輪推理
```

`world/` 套件擁有遊戲實體與規則（不依賴 LLM）。`npc/` 套件擁有使用案例迴圈。`llm/openai/` 是可替換的供應商轉接器。

`testdata/scenarios/tavern.json` 是參考酒館場景：謹慎的商人 Mira 持有治療藥水；玩家對她聲望不佳；北邊的路上有強盜。

### 範例：從命令列跑一次 NPC 推理

`cmd/stoa` 是一支小型 CLI，會載入場景 JSON，使用與測試相同的 `npc.Agent` 迴圈搭配確定性的腳本化推理引擎，最後輸出一份 JSON 報告。不需要 `OPENAI_API_KEY` 或網路。

```bash
go run ./cmd/stoa npc-run testdata/scenarios/tavern.json --actor mira
```

腳本化引擎在第一輪會故意提出一個無效意圖（贈送角色不持有的物品）。world 驗證器拒絕後，迴圈會把錯誤以具型別事件回灌，引擎在下一輪修正。最終 JSON 會包含：

- 來自場景檔的 `scenario` 與 `summary`
- `actor` 與 `task`
- `turns` 次數、最終 `intent`、結果 `observation`
- 完整 `events` 軌跡與彙整過的 `feedback` 驗證/執行錯誤

可用 `--task` 覆寫情境敘述，`--max-turns` 限制迴圈次數。

---

## 範例：記帳代理

會計切片把相同架構套用到複式記帳。一個自然語言請求被轉換成經驗證的分錄——代理提出具型別的 `JournalIntent`，會計領域驗證它，只有平衡、期間正確、科目有效的分錄才會被過帳到帳本。

```text
記帳請求
→ LLM 提出 JournalIntent（科目、金額、期間）
→ accounting.Validator 執行會計不變條件
→ 通過驗證的分錄以 JournalPosted 事件發布，並投影到帳本
→ 驗證錯誤以具型別事件回饋以供自我修正
```

`accounting/` 擁有領域模型——科目表、會計期間、分錄和驗證規則——不依賴任何 LLM。`bookkeeper/` 擁有使用案例迴圈和功能專屬的提示詞渲染器。

### 範例：從命令列建立一筆記帳分錄

`book-run` 會從工作目錄（預設 `~/.flarex/stoa`，或用 `--work-dir <dir>`
指定）讀取 `config.yaml`。檔案必須存在；空檔案也有效，會選用全離線的
預設值——記憶體持久層、行程內事件匯流排、腳本化推理引擎。需要
Postgres + NATS + 真實 LLM 時，複製 [`config.example.yaml`](config.example.yaml)。

```bash
# 一次性設定：空的 config.yaml 會選用全離線預設值。
mkdir -p ~/.flarex/stoa && touch ~/.flarex/stoa/config.yaml

# 離線範例：腳本引擎第一輪會故意提出一個不平衡分錄，
# 讓迴圈一定會走過驗證回饋循環。
go run ./cmd/stoa book-run testdata/accounting/aws_bill.json \
  --request "Paid AWS bill 100 USD using company credit card"

# 真實打 OpenAI API。--engine 與 --model 會覆寫 config.yaml 的 llm 區塊。
OPENAI_API_KEY=sk-... go run ./cmd/stoa book-run \
  testdata/accounting/aws_bill.json \
  --engine openai \
  --model gpt-5.4-mini \
  --request "Paid AWS bill 100 USD using company credit card on 12 May 2026"
```

JSON 輸出包含：`request`、`turns`、已過帳的 `entry`、最終 `intent`、完整 `events` 軌跡，以及驗證錯誤的 `feedback` 彙整。

---

## 專案結構

Stoa 依照**功能切片**組織程式碼，而不是依照架構層級。每個功能會切成領域套件和代理套件，讓領域模型可以獨立被匯入，同時讓代理迴圈保持明確。

```text
stoa/
├── cmd/
│   └── stoa/              # 範例 CLI（npc-run、book-run 子指令）
├── world/                 # 遊戲領域：世界狀態、角色、物品、NPCIntent、驗證器
├── npc/                   # NPC 使用案例迴圈與提示詞渲染
├── accounting/            # 會計領域：帳本、科目、期間、驗證器、事件
├── bookkeeper/            # 記帳代理迴圈、提示詞渲染、事件 port
├── persistence/           # LedgerRepository 轉接器（memory、postgres）
├── messaging/             # EventBus 轉接器（inproc、nats）
├── config/                # cmd/stoa 的 config.yaml 載入器
├── harness/
│   └── loop/              # 具型別的推理、驗證、執行 runner
├── llm/                   # 共用推理合約與提示詞渲染
│   └── openai/            # OpenAI 供應商轉接器
├── testdata/
│   ├── scenarios/         # NPC 場景樣本（例如 tavern.json）
│   └── accounting/        # 記帳場景樣本（例如 aws_bill.json）
└── docs/
    └── architecture.md
```

未來的功能也應該遵循同樣形狀：領域套件放業務概念與不變條件，代理套件放流程編排與功能專屬提示詞。供應商轉接器應該留在功能套件外，除非該功能真的擁有那個基礎設施。

### 為什麼依功能切片，而不是依層級切

Go 的慣例是依照套件提供什麼能力來組織，而不是依照裡面放了什麼類型。`models/`、`services/`、`repositories/` 這種切法會把同一個業務概念分散到多個目錄；修改一個功能時要到處跳。功能套件把相關程式碼放在一起，依賴方向則透過**介面**表達，而不是靠目錄結構硬撐。

---

## 快速開始

> ⚠️ Stoa 還在早期開發階段。API 可能會改變。

### 前置需求

- Go 1.25+
- 模型供應商 API key 或 OAuth

### 安裝

```bash
git clone https://github.com/flarexio/stoa.git
cd stoa
go mod download
```

### 執行測試

```bash
go test ./...
```

跑完所有單元與離線測試。不需要 API key 或網路。

OpenAI 整合測試由兩個環境變數共同控制——兩個都要設定才會執行：

```bash
STOA_RUN_OPENAI_TESTS=1 OPENAI_API_KEY=sk-... \
  go test -v -run TestAgent_OpenAI ./bookkeeper/...
```

`STOA_RUN_OPENAI_TESTS` 閘門確保 `go test ./...` 即使在環境中已有 `OPENAI_API_KEY` 的情況下，也不會默默消耗 API tokens。

---

## Stoa 不是什麼

- **不是框架。** 你讀得懂程式碼，也擁有程式碼。
- **不是 LangChain 替代品。** 它是不同類型的東西。
- **不是通用工具。** 先從狹窄領域開始，等模式出現再泛化。
- **不是魔法。** 每個決策都是明確的。每個抽象都要值得存在。

---

## 貢獻

Stoa 目前是個人工作坊專案。歡迎開 issues 和 discussions；等核心架構穩定後會再考慮 PR。

如果你對這個理念或做法有興趣，歡迎開 discussion。

---

## 授權

MIT — see [LICENSE](LICENSE)。

---

## 致謝

- **季蒂昂的芝諾**，因為他曾在廊柱下講學。
- **王陽明**，因為他堅持知行合一。
- **Anthropic 的 *Building Effective Agents***，因為它清楚說明了為什麼大多數代理程式碼不需要框架。

---

<p align="center">
  <i>Control what you can. Harness what you cannot. Let the work itself be the teacher.</i>
</p>
