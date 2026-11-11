# Mobil elérés, 1. szakasz: szerver-mag – megvalósítási terv

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Cél:** az asmgr kapjon egy saját webszervert, amely csak a gépen figyel, és amelyhez egy telefon QR-kódos párosítással, visszavonható eszköz-belépéssel csatlakozhat. Ez indítható a desktopban egy kapcsolóval, vagy ablak nélkül `--serve` módban.

**Architektúra:** új `mobile/` csomag. Benne van az eszköz- és párosítás-tár (`devices.go`) és a HTTP-szerver a védelmi rétegekkel (`server.go`). A desktop `App` indítja és állítja le a beállítás szerint; a fej nélküli mód ugyanezt teszi Wails nélkül. Egyszerre csak a projektzár tulajdonosa szolgálhat ki, ez a meglévő `Storage.LockProjectForUse`, amire a desktop is épít.

**Tech stack:** Go `net/http` (új függőség nélkül), QR-kép: `github.com/skip2/go-qrcode` (MIT), frontend: Svelte a meglévő Beállítások ablakban.

**Spec:** a 2026-11-09-i beszélgetés terve és a `memory/remote-access-research.md`. Ez a terv a 2–5. szakaszt nem tartalmazza: mobil API, PWA, értesítési link, Tailscale-útmutató a felületen.

## Global Constraints

- A szerver kizárólag `127.0.0.1`-re köt. Más címre kötni nem lehet, nincs rá beállítás.
- Alapport: `9760`, a Beállításokban módosítható (1024–65535).
- Párosító kód: egyszer használható, **5 perc** után lejár, legalább 128 bit véletlen, URL-biztos.
- Eszköz-titok: 256 bit véletlen. Lemezre csak a SHA-256 hash kerül (`devices.json`, 0600, a konfig-könyvtárban). Az összehasonlítás konstans idejű.
- Süti: `asmgr_device`, `HttpOnly`, `SameSite=Strict`, `Path=/`. `Secure`, ha a kérés HTTPS-proxyn jött (`X-Forwarded-Proto: https`).
- Sikertelen párosítás: 10 hibás kísérlet után 15 percig minden párosítás tiltott.
- A desktop 301 Wails-metódusából egyik sem érhető el HTTP-n.
- Új függőség csak a `go-qrcode`.
- Tesztek: soha nem a valódi `~/.config/agent-session-manager-desktop` (helyette `guardedTestStorage`, `t.TempDir()`); valódi tmux csak izolált szerverrel.
- Minden felhasználói szöveg mind a 20 nyelvi fájlba (`frontend/src/lib/i18n/locales/*.json`).
- Commit csak a user kérésére.

## Review Focus

1. **A `tailscale serve` a saját `Host`-ját küldi** (`<gép>.<tailnet>.ts.net`). A Host-ellenőrzés ezt engedje, de egy tetszőleges idegen Host-ot ne. Teszt: 2. feladat, `TestHostsAllowed`.
2. **A port foglalt** (egy másik program vagy egy második asmgr). Indításkor érthető hiba kell, nem pánik, és a kapcsoló álljon vissza. Teszt: 2. feladat, `TestStartOnBusyPortFails`; 3. feladat, frontend-teszt.
3. **Már felhasznált vagy lejárt kódot küldenek újra.** Ilyenkor nem lehet belépés, és a hibaszámláló nő. Teszt: 1. feladat.
4. **A desktop már fut, és elindul a `--serve`.** A második ne induljon el, és mondja meg, melyik PID tartja a zárat. Teszt: 4. feladat, `TestServeRefusesWithoutProjectLock`.
5. **Visszavont eszköz élő sütivel.** A következő kérés már 401. Teszt: 2. feladat, `TestRevokedDeviceIsRejected`.

---

### 1. feladat: Eszköz- és párosítás-tár

**Fájlok:**
- Létrehozás: `mobile/devices.go`
- Teszt: `mobile/devices_test.go`

**Interfészek:**
- Termel:
  - `type Device struct { ID, Name string; CreatedAt, LastSeen time.Time }` (a JSON-ban titok nincs)
  - `func OpenDevices(path string) (*Devices, error)`
  - `func (d *Devices) NewPairing(now time.Time) (code string, expires time.Time, err error)` – egyszerre egy élő kód; új kérés felülírja a régit.
  - `func (d *Devices) Redeem(code, name string, now time.Time) (device Device, secret string, err error)` – hibák: `ErrPairingInvalid`, `ErrPairingLocked`.
  - `func (d *Devices) Authenticate(secret string, now time.Time) (Device, bool)` – `LastSeen` frissítése legfeljebb percenként ír lemezre.
  - `func (d *Devices) List() []Device`, `func (d *Devices) Revoke(id string) error`

- [ ] **1. lépés: a bukó tesztek megírása:**
  - `TestPairingWorksOnceWithinFiveMinutes`: a kód 4:59-kor működik, másodszor `ErrPairingInvalid`.
  - `TestExpiredPairingFails`: 5:01-kor `ErrPairingInvalid`.
  - `TestNewPairingReplacesTheOld`: a régi kód érvénytelen.
  - `TestTenFailuresLockPairingForFifteenMinutes`: a 11. próbálkozás még jó kóddal is `ErrPairingLocked`; 15 perc után működik.
  - `TestSecretIsStoredOnlyHashed`: a `devices.json` nem tartalmazza a titkot, a módja `0600`.
  - `TestAuthenticateAndRevoke`: jó titok → eszköz; rossz → `false`; `Revoke` után `false`; újranyitás után is.
- [ ] **2. lépés:** `go test ./mobile/ -run 'Pairing|Secret|Authenticate'` → FAIL (nincs csomag).
- [ ] **3. lépés: megvalósítás** `mobile/devices.go`-ban. Véletlen: `crypto/rand`, kódolás: `base64.RawURLEncoding`. Mentés: atomi átnevezéssel, a `passwords` csomag `save` mintájára.
- [ ] **4. lépés:** ugyanaz a parancs → PASS; `go test -race ./mobile/`.

### 2. feladat: HTTP-szerver a védelmi rétegekkel

**Fájlok:**
- Létrehozás: `mobile/server.go`
- Teszt: `mobile/server_test.go`

**Interfészek:**
- Fogyaszt: `*Devices` (1. feladat).
- Termel:
  - `type Server struct{...}`
  - `func NewServer(devices *Devices, port int) *Server`
  - `func (s *Server) Start() error` – `127.0.0.1:port`; foglalt portnál a hiba `ErrPortInUse`.
  - `func (s *Server) Stop(ctx context.Context) error`
  - `func (s *Server) Handler() http.Handler` (a tesztekhez)
  - Útvonalak:
    - `POST /api/pair` `{code,name}` → süti + `{deviceId}`
    - `GET /api/me` → `{id,name}` (belépés kell)
    - `POST /api/logout`
    - `GET /` → ideiglenes oldal: „Párosítva / nincs párosítva”, a 3. szakasz PWA-ja váltja.

- [ ] **1. lépés: a bukó tesztek megírása** (`httptest`):
  - `TestNonLoopbackPeerIsRefused`: `RemoteAddr = 192.168.1.5:1234` → 403.
  - `TestHostsAllowed`: `127.0.0.1:9760`, `localhost:9760`, `gep.tail1234.ts.net` → átmegy; `evil.example.com` → 421.
  - `TestCrossOriginWriteIsRefused`: `POST /api/pair` idegen `Origin`-nel → 403; azonos origin vagy hiányzó Origin → feldolgozza.
  - `TestPairSetsStrictCookie`: `HttpOnly`, `SameSite=Strict`; `X-Forwarded-Proto: https` esetén `Secure` is.
  - `TestMeNeedsDevice`: süti nélkül 401, párosítás után 200 a névvel.
  - `TestRevokedDeviceIsRejected`: `Revoke` után a régi süti 401.
  - `TestStartOnBusyPortFails`: a portot előbb a teszt foglalja → `ErrPortInUse`.
  - `TestBodyLimit`: 64 KiB fölötti törzs → 413.
- [ ] **2. lépés:** `go test ./mobile/ -run 'Loopback|Hosts|CrossOrigin|Cookie|Me|Revoked|Busy|Body'` → FAIL.
- [ ] **3. lépés: megvalósítás.** A köztes rétegek sorrendje: loopback-peer → Host → Origin (nem-GET kérésnél) → törzs-limit → útvonal. A Host-lista: a két loopback név a portonként, plusz minden `*.ts.net`. Az Origin-lista ugyanez sémával: `http://127.0.0.1:<port>`, `http://localhost:<port>`, `https://*.ts.net`. Nem vehetjük biztosra, hogy a `tailscale serve` a saját Host-ját továbbítja vagy átírja, ezért az Origin nem a Host-tal, hanem ezzel a listával vetendő össze. A tényleges viselkedést a Tailscale telepítése után élesben ellenőrizni kell.
- [ ] **4. lépés:** PASS, `-race`-szel is.

### 3. feladat: Desktop-kapcsoló, párosítás és eszközök a Beállításokban

**Fájlok:**
- Módosítás:
  - `session/storage.go` – `Settings`: `MobileEnabled bool \`json:"mobile_enabled,omitempty"\``, `MobilePort int \`json:"mobile_port,omitempty"\``
  - `app.go` – indítás a `startup` végén, ha a beállítás be van kapcsolva és az app a projektzár tulajdonosa; leállítás a `shutdown`-ban
  - a Beállítások-ablak komponense (a meglévő szakaszok mintájára)
  - 20 nyelvi fájl
- Létrehozás:
  - `mobile_api.go` (az `App` Wails-metódusai)
  - `mobile_api_test.go`
  - `frontend/tests/browser/mobile-settings.spec.mjs` (+ fixture a meglévők mintájára)
- `go.mod`: `github.com/skip2/go-qrcode`

**Interfészek:**
- Fogyaszt: `mobile.OpenDevices`, `mobile.NewServer`, `Start`/`Stop`, `NewPairing`, `List`, `Revoke`.
- Termel (Wails):
  - `SetMobileAccess(enabled bool, port int) (MobileStatus, error)`
  - `GetMobileStatus() MobileStatus` – `{running bool, port int, error string}`
  - `CreateMobilePairing() (MobilePairing, error)` – `{code, url, qrPNG string(base64), expiresAt}`; az `url` a `http://127.0.0.1:<port>/#pair=<code>`. A Tailscale-es címet a 4. szakasz adja hozzá.
  - `ListMobileDevices() []mobile.Device`, `RevokeMobileDevice(id string) error`

- [ ] **1. lépés: a bukó Go-tesztek:**
  - `TestMobileAccessStartsAndStops`: szabad porton bekapcsolva elérhető a `GET /`; kikapcsolva a port felszabadul.
  - `TestMobileAccessNeedsTheProjectLock`: `projectLocked=false` → hiba, nem indul.
  - `TestMobileSettingSurvivesRestart`: a beállítás megmarad.
  - `TestBusyPortTurnsTheSwitchBackOff`: foglalt port → hiba, és `MobileEnabled` hamis marad.
  - `TestPairingQRDecodesToTheURL`: a PNG tényleg QR, a mérete legalább 256 px.
- [ ] **2. lépés:** `go test . -run 'MobileAccess|MobileSetting|BusyPort|PairingQR'` → FAIL.
- [ ] **3. lépés: Go-oldal megvalósítása;** `wails generate module -tags webkit2_41`, normalizálás.
- [ ] **4. lépés: a bukó Playwright-teszt** (`mobile-settings.spec.mjs`):
  - a kapcsoló hívja a `SetMobileAccess(true, 9760)`-et;
  - a hiba megjelenik, és a kapcsoló visszaáll;
  - a „Párosítás” gomb QR-képet és lejárati időt mutat;
  - az eszközlistában a visszavonás megerősítés után hívja a `RevokeMobileDevice`-ot.
- [ ] **5. lépés: frontend megvalósítása.** Szakasz a Beállításokban:
  - kapcsoló, port;
  - figyelmeztetés: „aki párosít, a te nevedben gépelhet a terminálokba”;
  - Tailscale-parancs: `tailscale serve --bg http://127.0.0.1:<port>`;
  - párosítás gomb, eszközlista.
  - Minden `p-select`-szerű elem `appendTo="body"`, ha támogatja.
- [ ] **6. lépés:** `npm run check` (0 hiba), `npm test`, `npx playwright test tests/browser/mobile-settings.spec.mjs` → PASS.

### 4. feladat: Fej nélküli mód (`--serve`)

**Fájlok:**
- Módosítás:
  - `main.go` – a `wails.Run` előtt: ha `--serve` az argumentum, akkor `runHeadless(...)`, és kilép
  - `app.go` és a többi `runtime.EventsEmit` hívó (17 hely) – egy `a.emit(name string, data ...interface{})` segédre cserélve, ami fej nélkül nem csinál semmit
- Létrehozás:
  - `headless.go`
  - `headless_test.go`

**Interfészek:**
- Fogyaszt: 1–3. feladat; `Storage.LockProjectForUse`.
- Termel:
  - `func runHeadless(args []string) int` – opciók: `--port N`, `--pair` (indításkor kiír egy párosító kódot és URL-t a terminálra); SIGINT/SIGTERM esetén rendezett leállás.
  - `func (a *App) emit(name string, data ...interface{})`

- [ ] **1. lépés: a bukó tesztek:**
  - `TestEmitIsSilentWithoutWindow`: `App{}` nil `ctx`-szel → nincs pánik.
  - `TestServeRefusesWithoutProjectLock`: a zárat egy másik PID tartja → nem nulla kilépési kód, és az üzenetben benne van a PID.
  - `TestServePrintsAPairingCode`: `--pair` → a kimenetben egy 5 percig érvényes kód és az URL.
  - `TestNoWailsCallInHeadlessPaths`: forrásszintű teszt, ami tiltja a közvetlen `runtime.EventsEmit` hívást az `emit`-en kívül.
- [ ] **2. lépés:** `go test . -run 'Emit|Serve|NoWails'` → FAIL.
- [ ] **3. lépés: megvalósítás.** A `runHeadless` ugyanazokat a háttérfolyamatokat indítja, mint a `startup`: sweep, figyelők, a terminál-WS. A Wails-specifikus részeket, például az ablakméret-visszaállítást, kihagyja.
- [ ] **4. lépés:**
  - a teljes Go-kapu: `go vet ./... && go test -race ./...`;
  - Windows: `go vet` a megszokott keresztfordítással;
  - kézi próba: `asmgr-desktop --serve --pair` egy második projekten, majd `curl -i http://127.0.0.1:9760/`.

## Végső ellenőrzés
- [ ] Teljes kapu: Go (race), Windows vet, `npm run check`, `npm test`, teljes Playwright.
- [ ] Buildek (dev → sima → `-run` másolat), a `.38`-ra is.
- [ ] Kézi próba: kapcsoló be, QR olvasása telefonnal. Tailscale nélkül ez csak a gépről megy; a telefonos próba a Tailscale telepítése után jön.
- [ ] Commit csak a user kérésére.
