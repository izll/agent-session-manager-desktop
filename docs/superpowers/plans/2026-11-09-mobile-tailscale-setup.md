# Mobil elérés: Tailscale-beállítás varázsló – megvalósítási terv

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Cél:** a Beállítások → Mobil elérés szakasz lépésenként végigvezeti a felhasználót a Tailscale telepítésén, a bejelentkezésen, a `tailscale serve` beállításán és a telefon párosításán. Minden lépésnél megmutatja, hol tart, és egy gombbal elindítja a következőt.

**Architektúra:**
- Az állapotot a háttér olvassa ki a `tailscale` parancsból: telepítve van-e, mi a `status --json` és a `serve status --json` eredménye.
- A `sudo`-t igénylő lépések egy új, helyi terminálfülben futnak. Ez ugyanaz a megoldás, mint a Bitwarden-bejelentkezésnél: a jelszót a felhasználó írja be, és siker után a fül bezárul.
- macOS-en és Windowson a telepítés és a bejelentkezés a Tailscale saját alkalmazásában történik; ott a varázsló a letöltőoldalt nyitja meg.

**Tech stack:** Go (a meglévő `runCommandInNewTab`, `session.TabCloseLogin`), Svelte (`MobileAccessSection.svelte`).

**Spec:** a user kérése („végig tudod vezetni a felhasználót a programból?”, kérdések nélkül), és a `docs/superpowers/plans/2026-11-09-mobile-access-phase1.md` folytatása.

## Global Constraints

- A parancsok rögzítettek. Felhasználói vagy hálózatról jövő szöveg nem kerülhet beléjük; a port csak egész szám (1024–65535).
- Linux-parancsok:
  - telepítés: `curl -fsSL https://tailscale.com/install.sh | sh`
  - bejelentkezés: `sudo tailscale up --operator=$USER`
  - kiszolgálás: `tailscale serve --bg http://127.0.0.1:<port>`
- macOS/Windows: telepítés és bejelentkezés a `https://tailscale.com/download` oldalon. A kiszolgálás parancsa ugyanaz, ha a `tailscale` parancs elérhető.
- Lépés csak helyi fülből indítható. Távoli fülből a hiba: `errBitwardenLoginNeedsLocalTab` mintájára egy új, saját hibakulcs.
- Minden új felhasználói szöveg mind a 20 nyelven.
- Commit csak a user kérésére.

## Review Focus

1. **A Tailscale fut, de a `serve` egy másik portra proxyz.** A „kiszolgálás” lépés ne legyen kész. Teszt: 1. feladat, `TestServeStatusPointsAtOurPort`.
2. **A Tailscale telepítve van, de nincs bejelentkezve** (`NeedsLogin`, `Stopped`, `NoState`). A varázsló a bejelentkezés lépésnél álljon. Teszt: 1. feladat, `TestSetupStepFollowsTheState`.
3. **A mobil elérés ki van kapcsolva.** A kiszolgálás lépése ezt kérje először, mert port nélkül nincs mit kiszolgálni. Teszt: 1. feladat.
4. **A lépés távoli fülből indul.** Ilyenkor ne fusson a szerveren. Teszt: 2. feladat, `TestSetupStepRefusedOnAServerTab`.
5. **A `tailscale` parancs lassú vagy lefagy.** Az állapotlekérés legfeljebb 3 másodpercig tartson, és ne fagyassza le a felületet. Teszt: 1. feladat, `TestSetupStatusGivesUpOnASlowTailscale`.

---

### Task 1: A beállítás állapota

**Fájlok:**
- Létrehozás: `mobile_setup.go`, `mobile_setup_test.go`

**Interfészek:**
- Termel:
  - `type MobileSetup struct { Step string; Installed, LoggedIn, Serving bool; TailnetName string; Platform string; Commands map[string]string }`
    - JSON-mezők: `step`, `installed`, `loggedIn`, `serving`, `tailnetName`, `platform`, `commands`
    - `Step` értékei: `"install"`, `"login"`, `"enable"`, `"serve"`, `"phone"`
    - `Commands` kulcsai: `install`, `login`, `serve` (macOS/Windowson az `install` és a `login` üres)
  - `func (a *App) GetMobileSetup() MobileSetup` (Wails)
  - tisztán tesztelhető részek:
    - `func setupStep(installed bool, backend string, mobileOn bool, serving bool) string`
    - `func serveProxiesTo(serveStatus []byte, port int) bool`
    - `func setupCommands(goos string, port int) map[string]string`
  - teszt-csere: `var tailscaleCommand = func(ctx context.Context, args ...string) ([]byte, error)` (a meglévő `tailscaleStatus` is erre épül át)

- [ ] **1. lépés: a bukó tesztek:**
  - `TestSetupStepFollowsTheState` – a sorrend: nincs telepítve → `install`; `NeedsLogin`/`Stopped`/`NoState` → `login`; `Running`, de a mobil elérés ki van kapcsolva → `enable`; `Running`, be van kapcsolva, nem proxyz → `serve`; minden kész → `phone`.
  - `TestServeStatusPointsAtOurPort` – `{"Web":{"gep.tail1.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:9760"}}}}}` → 9760-ra igaz, 9761-re hamis; üres vagy hibás JSON-ra hamis.
  - `TestSetupCommandsArePinned` – Linuxon a fenti három pontos szöveg, 9760-as porttal; `darwin`-on az `install` üres, a `serve` kitöltött.
  - `TestSetupStatusGivesUpOnASlowTailscale` – a csere 10 másodpercig blokkol; a `GetMobileSetup` 4 másodpercen belül visszatér, `Installed:false`.
- [ ] **2. lépés:** `go test . -run 'SetupStep|ServeStatus|SetupCommands|SetupStatus'` → FAIL.
- [ ] **3. lépés: megvalósítás.**
  - A „telepítve” jelentése: `exec.LookPath("tailscale")`, macOS-en még a `/Applications/Tailscale.app/Contents/MacOS/Tailscale` útvonal is.
  - Minden `tailscale` hívás 3 másodperces kontextussal fut.
- [ ] **4. lépés:** PASS, `-race`-szel is.

### Task 2: Lépés indítása terminálfülben

**Fájlok:**
- Módosítás: `mobile_setup.go`, `mobile_setup_test.go`

**Interfészek:**
- Fogyaszt: `setupCommands` (1. feladat), `runCommandInNewTab(sessionID, fromWindow, name, line, session.TabCloseLogin, project)`, `tabRunsOnThisMachine`.
- Termel: `func (a *App) RunMobileSetupStep(step, sessionID string, fromWindow int, expectedProjectID string) (int, error)`
  - a `step` csak `install`, `login` vagy `serve` lehet; másra `errUnknownSetupStep`;
  - üres parancs (macOS/Windows telepítés) esetén `errSetupStepInApp`;
  - siker esetén az új fül indexe.

- [ ] **1. lépés: a bukó tesztek:**
  - `TestSetupStepRefusedOnAServerTab` – szerveren futó munkamenetnél `errBitwardenLoginNeedsLocalTab`-szerű hibát ad, és fül nem nyílik.
  - `TestUnknownSetupStepIsRefused` – `"rm"`, `""` → `errUnknownSetupStep`.
  - `TestSetupStepLineEndsTheTab` – a beírt sor a parancs, utána ` && exit` (izolált tmux-szal, a `tab_close_on_exit_unix_test.go` mintájára: a fül a sor végén kilép, ha a parancs sikerült; hamis paranccsal: `true`).
- [ ] **2. lépés:** FAIL.
- [ ] **3. lépés: megvalósítás.** A `runCommandInNewTab` meglévő bezáró mechanizmusa (`TabCloseLogin`) zárja a fület; a fül neve: `Tailscale`.
- [ ] **4. lépés:** PASS.

### Task 3: A varázsló a Beállításokban

**Fájlok:**
- Módosítás:
  - `frontend/src/lib/components/Dialogs/MobileAccessSection.svelte`
  - `frontend/src/App.svelte` (a `tabs:closed-on-exit` eseményre a varázsló újraellenőriz – egy kis store-on át)
  - 20 nyelvi fájl
  - `frontend/tests/browser/mobile-settings-fixture.ts`, `mobile-settings.spec.mjs`
- Új store: `frontend/src/lib/stores/mobileSetup.ts` – `mobileSetupRecheck` (szám, amit az esemény növel).

**Interfészek:**
- Fogyaszt: `GetMobileSetup`, `RunMobileSetupStep` (Wails), `BrowserOpenURL` a macOS/Windows letöltéshez.

- [ ] **1. lépés: a bukó Playwright-tesztek:**
  - `'the steps show where setup stands'` – a fixture állapota `login`: a telepítés ✓, a bejelentkezés aktív a gombjával, a többi még hátravan.
  - `'the current step's button runs it in a tab'` – a gomb a `RunMobileSetupStep('login', 's1', 0, 'p1')` hívást indítja.
  - `'enable is the switch itself'` – az `enable` lépésnél a gomb a meglévő kapcsolót kapcsolja be.
  - `'recheck asks again'` – az „Újraellenőrzés” gomb újra lekéri a `GetMobileSetup`-ot.
  - `'on a Mac the install step opens the download page'` – `platform:'darwin'` esetén a gomb a `BrowserOpenURL('https://tailscale.com/download')`-ot hívja.
  - `'at the phone step pairing is offered'` – a `phone` lépésnél a telefonos utasítás és a párosító gomb látszik.
- [ ] **2. lépés:** FAIL.
- [ ] **3. lépés: megvalósítás.**
  - Számozott lista, lépésenként ✓ / aktív / hátralévő állapottal. A szövegek:
    - „A Tailscale telepítése”
    - „Bejelentkezés a Tailscale-be”
    - „A mobil elérés bekapcsolása”
    - „Kiszolgálás a tailneten”
    - „A telefon: Tailscale-alkalmazás, ugyanaz a fiók, majd a QR-kód beolvasása”
  - Az aktív lépésnél: gomb, egy rövid magyarázat és a parancs másolható formában.
  - Ha nincs kiválasztott helyi munkamenet, a gomb inaktív, és csak a parancs látszik.
- [ ] **4. lépés:** `npm run check` (0 hiba), `npm test`, a teljes Playwright → PASS.

## Végső ellenőrzés
- [ ] Go race, Windows vet, frontend, teljes Playwright.
- [ ] Buildek, `.38`-ra is.
- [ ] Független átnézés a teljes változtatásra.
- [ ] Commit csak kérésre.
