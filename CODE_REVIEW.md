# ASMGR Desktop – code review

Dátum: 2026-07-10

Vizsgált állapot: az aktuális munkafa, beleértve a már meglévő, nem commitolt módosításokat is.

Hatókör: Go backend, session- és terminálkezelés, diktálás, MCP, updater, Svelte/TypeScript frontend, build és tesztek.

## Javítási státusz – 2026-07-10

Az ebben a riportban azonosított correctness, concurrency, security, lifecycle, updater/release és frontend type-check hibák javítása elkészült az aktuális munkafában. A findingek alábbi részletes leírásai az eredeti hibás állapotot dokumentálják.

Az implementációhoz regressziós tesztek készültek a diktálási konkurenciakezeléshez, MCP transport hibákhoz és nagy válaszokhoz, Git diff index-érintetlenséghez, projekt-ID path traversalhöz, valamint az updater SemVer-, checksum- és artifact-kezeléséhez. Új PR/push CI workflow is készült.

Végső ellenőrzés:

- `go test -race ./...`: sikeres;
- `go vet ./...`: sikeres;
- `npm run check`: sikeres, 0 error (a korábbi nem blokkoló a11y/unused warningok megmaradtak);
- `npm run build`: sikeres;
- workflow YAML parse és `git diff --check`: sikeres.

## Vezetői összefoglaló

A projekt funkcionálisan gazdag, a terminálkapcsolat több korábbi reconnect- és teljesítményproblémára láthatóan tudatos védelmet tartalmaz. A Go kód lefordul, a meglévő Go tesztek és a production frontend build sikeresek. Ugyanakkor a diktálási alrendszerben több kritikus konkurenciakezelési hiba van, a frontendben pedig két olyan versenyhelyzet, amely felhasználói adatot írhat rossz sessionbe vagy duplikált sessiont hozhat létre. A frontend típusellenőrzés jelenleg 14 hibával bukik, a teljes Go statement coverage pedig lényegében 1% alatti.

Javasolt sorrend:

1. Diktálási channel/buffer versenyhelyzetek és a Notes adatfelülírás javítása.
2. Session-létrehozás dupla submitjának megszüntetése.
3. MCP kliens életciklusának és üzenetméretének robusztussá tétele.
4. Updater verziókezelésének és integritás-ellenőrzésének javítása.
5. Frontend aszinkron request-race-ek, majd a típusellenőrzési hibák rendezése.
6. Célzott tesztek hozzáadása a fenti regressziókra.

## Megállapítások

### Kritikus prioritás

#### 1. Diktálás leállításakor `send on closed channel` panic lehetséges

**Hely:** [`dictation/streaming_recognizer.go:74`](dictation/streaming_recognizer.go#L74), [`dictation/streaming_recognizer.go:107`](dictation/streaming_recognizer.go#L107)

`SendAudio` mutex alatt kiolvassa az `isRunning` értékét, majd elengedi a lockot és csak ezután küld az `audioChan` csatornára. Közben a `Stop` false-ra állíthatja az állapotot és bezárhatja ugyanazt a csatornát. A `SendAudio: true olvasás → Stop: close → SendAudio: send` sorrend panicot okoz.

**Javaslat:** a producer ne küldhessen bezárt csatornára; context/done csatornás leállítás és egyetlen channel-owner használata, vagy a channel bezárásának elhagyása. Erre célzott konkurenciateszt szükséges.

#### 2. Az audio buffer konkurens írása/olvasása adatversenyt és korrupt hangot okozhat

**Hely:** [`dictation/audio_capture.go:333`](dictation/audio_capture.go#L333), [`dictation/audio_capture.go:560`](dictation/audio_capture.go#L560), [`dictation/speech_recognizer.go:884`](dictation/speech_recognizer.go#L884)

A recording goroutine lock nélkül ír a `bytes.Buffer`-be, miközben a getterek `ac.mu` alatt olvassák vagy resetelik. A mutex így csak az olvasókat védi egymástól, az írótól nem. A `GetAudioData` ráadásul a buffer belső slice-át adja vissza másolás nélkül; a slice queue-ba kerül, a buffer resetje és következő írása pedig felülírhatja a még feldolgozatlan audio chunk backing arrayét.

**Hatás:** data race, hibás PCM, ismétlődő/korrupt streaming hang, szélsőséges esetben `bytes.Buffer` körüli panic.

**Javaslat:** minden buffer-hozzáférés ugyanazon lock alatt történjen, a fogyasztó pedig saját másolatot kapjon (`bytes.Clone` vagy explicit copy). A capture/consume tulajdonjogot érdemes channel-alapú immutable chunkokra egyszerűsíteni.

#### 3. Tabváltáskor a régi jegyzet az új tabra mentődhet

**Hely:** [`frontend/src/lib/components/MainPanel/Notes.svelte:57`](frontend/src/lib/components/MainPanel/Notes.svelte#L57), [`frontend/src/lib/components/MainPanel/Notes.svelte:80`](frontend/src/lib/components/MainPanel/Notes.svelte#L80), [`frontend/src/lib/components/MainPanel/Notes.svelte:98`](frontend/src/lib/components/MainPanel/Notes.svelte#L98)

A reactive session/window-váltó blokk meghívja a `saveNow()` függvényt, amely már a globális store-ból az **új** session- és window-azonosítót olvassa, miközben a `notes` még a régi tab tartalma. Függő debounce esetén így a régi jegyzet az új tabra kerülhet. Emellett a `loadNotes` válaszai sincsenek request/session identityvel védve, ezért egy lassabb régi kérés felülírhatja az új tab tartalmát.

**Javaslat:** szerkesztéskor rögzíteni kell a cél session/window ID-t és a mentendő snapshotot; betöltésnél generation counter vagy visszatéréskori ID-ellenőrzés szükséges.

#### 4. Enterrel dupla session-létrehozás indulhat, a resume-konfliktus ellenőrzése megkerülhető

**Hely:** [`frontend/src/lib/components/Dialogs/NewSessionDialog.svelte:155`](frontend/src/lib/components/Dialogs/NewSessionDialog.svelte#L155), [`frontend/src/lib/components/Dialogs/NewSessionDialog.svelte:202`](frontend/src/lib/components/Dialogs/NewSessionDialog.svelte#L202), [`frontend/src/lib/components/Dialogs/NewSessionDialog.svelte:256`](frontend/src/lib/components/Dialogs/NewSessionDialog.svelte#L256)

A form közvetlenül a `handleSubmit(fork: boolean)` függvényt kapja handlerként, ezért a Svelte `SubmitEvent` objektuma truthy `fork` értékként érkezik, és a `!fork` konfliktusvédelem kimarad. Az overlay keydown handler Enterre külön is meghívja a `handleSubmit()`-ot, majd a natív form submit még egyszer lefut. Nincs korai in-flight guard, így két párhuzamos létrehozás indulhat. A `svelte-check` ezt signature hibaként is jelzi.

**Javaslat:** paraméter nélküli form-submit wrapper, az Enter duplikált kezelőjének eltávolítása vagy `preventDefault`, valamint a handler elején `isSubmitting` guard.

### Magas prioritás

#### 5. Az MCP kliens nagy válasznál vagy child process hibánál tartósan használhatatlanná válik

**Hely:** [`mcp/client.go:145`](mcp/client.go#L145), [`mcp/client.go:204`](mcp/client.go#L204), [`mcp/client.go:211`](mcp/client.go#L211)

A stdout olvasó default `bufio.Scanner` limitet használ. Egy kb. 64 KiB-nál nagyobb `tools/list` vagy `tools/call` JSON sor leállítja a scanner ciklust; nincs `scanner.Buffer` és a `scanner.Err()` sincs feldolgozva. Ugyanez történik child exit/EOF esetén: a reader visszatér, de a `running` true marad, a pending requestek nem kapnak hibát, a cache pedig továbbra is a halott klienst adja vissza.

**Hatás:** minden későbbi hívás write hibával vagy akár 60 másodperces timeouttal végződhet, automatikus helyreállás nélkül.

**Javaslat:** emelt/maximált message limit vagy framed JSON decoder, scanner/EOF hiba kezelése, atomikus failed state, pending requestek lezárása hibával, cache invalidálás és szabályozott restart.

#### 6. A Google Speech API kérés korlátlan ideig blokkolhat

**Hely:** [`dictation/speech_recognizer.go:469`](dictation/speech_recognizer.go#L469)

Az API mód `http.Post`-ot, tehát timeout nélküli default klienst használ. Elakadt hálózatnál a kérés végtelen ideig függhet, miközben a hívási útvonal a recognition mutexet is tarthatja, így a feldolgozás és a lezáráskori flush is beragadhat.

**Javaslat:** explicit timeoutos `http.Client`, request context és cancellation; hálózati hívás közben ne legyen hosszú életű globális mutex tartva.

#### 7. Az updater lexikografikusan hasonlítja a verziókat

**Hely:** [`updater/updater.go:167`](updater/updater.go#L167)

A `latestVer > currentVer` string-összehasonlítás nem szemantikus verzió-összehasonlítás. Például `0.10.0` lexikografikusan kisebb `0.9.0`-nál, így a frissítés nem jelenik meg; más formátumoknál régebbi verzió is újabbnak látszhat.

**Javaslat:** validált semantic-version parser és összehasonlítás, prerelease szabályokkal; unit tesztek legalább `0.9→0.10`, patch, prerelease és `v` prefix esetekre.

#### 8. A frissítő ellenőrzés nélkül telepít letöltött binárist/csomagot

**Hely:** [`updater/updater.go:177`](updater/updater.go#L177), [`updater/updater.go:246`](updater/updater.go#L246), [`updater/updater.go:314`](updater/updater.go#L314)

A `.deb`, `.rpm` és tarball HTTPS-ről letöltődik, majd checksum vagy aláírás ellenőrzése nélkül települ; csomag esetén `sudo dpkg/rpm` fut rá. A fix, kiszámítható `/tmp/<filename>` útvonal használata ráadásul lokális symlink/race kockázatot is teremt.

**Javaslat:** release-manifestből származó SHA-256 és lehetőleg aláírás ellenőrzése telepítés előtt; `os.CreateTemp` privát ideiglenes könyvtárban; méretlimit és timeoutos kliens.

#### 9. A Task Master runtime-ban mindig a legfrissebb, nem rögzített npm csomagot futtatja

**Hely:** [`mcp/client.go:108`](mcp/client.go#L108)

Az `npx -y task-master-ai` verziómegkötés nélkül hálózatról tölthet le és futtathat új kódot. Ez reprodukálhatatlan működést, váratlan breaking change-et és szükségtelen supply-chain kitettséget okoz.

**Javaslat:** ellenőrzött verzió pinelése, lockolt/installált dependency használata, illetve a letöltés és első futtatás egyértelmű felhasználói kezelése.

#### 9/a. A macOS és Windows önfrissítő nem találja a release artifactban a várt binárist

**Hely:** [`updater/updater.go:326`](updater/updater.go#L326), [`.github/workflows/release.yml:181`](.github/workflows/release.yml#L181), [`.github/workflows/release.yml:257`](.github/workflows/release.yml#L257)

Az updater kizárólag `header.Name == "asmgr-desktop"` tar-bejegyzést keres. A macOS artifact egy teljes `asmgr-desktop.app` könyvtárfa, a Windows artifactban pedig `asmgr-desktop.exe` van. Mindkét platformon `binary not found in archive` várható; Windowson a futó exe helyben cseréje külön helper nélkül sem megbízható.

**Javaslat:** platformonként külön update stratégia: macOS-en teljes app bundle csere, Windowson kilépés után futó updater helper; a release tar szerkezetére integrációs teszt.

#### 9/b. A release tag verziója nem kerül bele a binárisba

**Hely:** [`version.go:3`](version.go#L3), [`wails.json:10`](wails.json#L10), [`.github/workflows/release.yml:44`](.github/workflows/release.yml#L44)

A workflow a tagből képezi az artifact/package verziót, de a bináris a hardcoded `Version` értékkel épül. Egy új tagből készült alkalmazás ezért továbbra is a régi verziónak vallhatja magát, és ugyanazt a frissítést ismét felajánlhatja.

**Javaslat:** build-time `-ldflags -X` verzióinjektálás és CI ellenőrzés a tag, Wails metadata és runtime verzió egyezésére.

### Közepes prioritás

#### 10. A read-only Diff nézet módosítja a felhasználó Git indexét

**Hely:** [`session/instance.go:1848`](session/instance.go#L1848), [`session/instance.go:1885`](session/instance.go#L1885)

Minden diff lekérés előtt `git add -N .` fut. Ez intent-to-add bejegyzéseket helyez az indexbe, tehát a megtekintés staging state-et módosít, és más git eszközök viselkedésére is hatással lehet.

**Javaslat:** untracked fájlokat külön `git ls-files --others --exclude-standard` úton kell megjeleníteni, vagy ideiglenes indexet (`GIT_INDEX_FILE`) használni.

#### 11. Projektnevekből path traversal-t tartalmazó projektazonosító készülhet

**Hely:** [`session/project.go:34`](session/project.go#L34), [`session/storage.go:97`](session/storage.go#L97), [`session/storage.go:258`](session/storage.go#L258)

A normalizálás csak space és underscore karaktert cserél; a `/` és `..` megmarad. Az ID közvetlenül `filepath.Join`-ba kerül, törléskor pedig `os.RemoveAll` fut a számított útvonalon. Megfelelő projektname így a `projects` könyvtáron kívülre irányíthatja az adatírást/törlést.

**Javaslat:** generált opaque UUID legyen az ID; minden bejövő ID-re allowlist regex és `filepath.Rel`/containment ellenőrzés, törlés előtt különösen.

#### 12. Sessionváltáskor régi aszinkron válaszok írhatják felül az új session UI-ját

**Hely:** [`frontend/src/lib/stores/tasks.ts:198`](frontend/src/lib/stores/tasks.ts#L198), [`frontend/src/lib/stores/tasks.ts:263`](frontend/src/lib/stores/tasks.ts#L263), [`frontend/src/lib/components/MainPanel/Diff.svelte:65`](frontend/src/lib/components/MainPanel/Diff.svelte#L65), [`frontend/src/lib/components/MainPanel/Preview.svelte:86`](frontend/src/lib/components/MainPanel/Preview.svelte#L86)

A task, Task Master status, Diff, Preview, TabBar window-list és Global Search kérések közül több feltétel nélkül ír globális/komponens state-et. Gyors session- vagy módváltásnál a lassabb régi válasz felülírhatja az újabbat. Taskoknál ennek nagyobb a kockázata: az A session taskjai látszódhatnak, miközben egy művelet már a B session ID-jával fut.

**Javaslat:** session-keyed store vagy minden kéréshez generation/request ID; válasz alkalmazása előtt a capture-ölt session/mode/query összevetése; pollingnál `setTimeout` az előző await után vagy in-flight guard.

#### 13. Frontend és backend diktálási settings modellje eltér

**Hely:** [`frontend/src/lib/components/MainPanel/TabBar.svelte:248`](frontend/src/lib/components/MainPanel/TabBar.svelte#L248), [`frontend/src/lib/components/MainPanel/TabBar.svelte:1243`](frontend/src/lib/components/MainPanel/TabBar.svelte#L1243)

A frontend olvassa és írja a `bufferSendEnter` mezőt, de az nincs a generált `DictationSettings` modellben/Go settings structban, és a tényleges send útvonal sem használja. A kapcsoló ezért nem perzisztál megbízhatóan és nincs hatása. Ez három konkrét `svelte-check` hibát is ad.

**Javaslat:** vagy end-to-end felvenni a mezőt a Go modellbe, generált bindingba és működésbe, vagy eltávolítani a félkész UI-t.

#### 14. Terminál reconnect után zombie `tmux attach` process maradhat

**Hely:** [`terminal_ws.go:453`](terminal_ws.go#L453)

A cleanup `cmd.Process.Kill()`-t hív, de `cmd.Wait()`-et nem. Unix rendszeren a childot a szülőnek reapelnie kell; gyakori reconnect mellett zombie processzek gyűlhetnek az alkalmazás kilépéséig.

**Javaslat:** kill után garantált `Wait`, egyetlen ownership ponttal és idempotens cleanup-pal.

#### 15. Érzékeny diktált szöveg feltétel nélkül logolódik

**Hely:** [`dictation_service.go:21`](dictation_service.go#L21), [`dictation_service.go:74`](dictation_service.go#L74)

A teljes felismert szöveg `%q` formában stdout/log fájlba kerül, függetlenül a debug logging beállítástól. Prompt, token, jelszó vagy személyes adat így tartós logban maradhat.

**Javaslat:** tartalom helyett csak hossz/állapot logolása; szöveges payload kizárólag explicit, rövid életű debug módban, jól látható figyelmeztetéssel.

#### 16. Konfigurációs és életciklus-problémák

- [`dictation/app_service.go:435`](dictation/app_service.go#L435): az `os.WriteFile(..., 0600)` meglévő fájl módját nem szigorítja, így egy korábbi 0644-es API-key settings fájl olvasható maradhat más helyi usernek. Írás előtt/után `Chmod(0600)` szükséges.
- [`dictation/audio_capture.go:215`](dictation/audio_capture.go#L215): ha `stream.Start()` hibázik, a már megnyitott stream nincs bezárva.
- [`app.go:124`](app.go#L124): normál shutdown nem állítja le a cache-elt Task Master MCP klienseket, így `npx` childok maradhatnak hátra.
- [`session/storage.go:191`](session/storage.go#L191): a `projects.json` read-modify-write CRUD nincs egységes mutex alatt; párhuzamos Wails hívások elveszett frissítést vagy közös `.tmp` fájl körüli hibát okozhatnak.
- [`updater/updater.go:35`](updater/updater.go#L35): a desktop updater cache API-ja a `~/.config/agent-session-manager` könyvtárat használná a desktop gyökér helyett. A cache/naplózó függvények jelenleg nincsenek bekötve, ezért ez félkész, halott funkció; későbbi bekötés előtt a config gyökeret egységesíteni kell.

#### 17. Release- és csomagolási hiányosságok

- [`build/nfpm.yaml:20`](build/nfpm.yaml#L20): a Linux csomag nem deklarálja legalább a bináris által közvetlenül igényelt PortAudio runtime csomagot (`libportaudio.so.2`). Minimális rendszeren az alkalmazás loader hibával el sem indulhat; tiszta Debian/Fedora install smoke tesztből kell előállítani a teljes natív dependency listát.
- [`.github/workflows/release.yml:82`](.github/workflows/release.yml#L82): a release CI nem futtatja az `npm run check` lépést, ezért a jelenlegi 14 type error ellenére kiadható build.
- [`.github/workflows/release.yml:35`](.github/workflows/release.yml#L35): manuális release-nél a tag létrehozási/push hibát `|| true` nyeli el. Létező tag esetén más branch HEAD-jéből épült artifact tölthető egy korábbi tag release-éhez.
- [`.github/workflows/release.yml:91`](.github/workflows/release.yml#L91): `npm install` és `nfpm@latest` miatt a release nem teljesen reprodukálható; `npm ci` és pinelt tool verziók indokoltak.
- [`updater/updater.go:221`](updater/updater.go#L221): GUI-ból az interaktív `sudo dpkg/rpm` általában nem kap TTY-t, ezért passwordless sudo nélkül a package-managed frissítés várhatóan elbukik. PolicyKit/helper vagy kézi package-manager flow szükséges.

## Build, statikus ellenőrzés és tesztek

| Ellenőrzés | Eredmény | Megjegyzés |
|---|---:|---|
| `go test ./...` | sikeres | Érdemi teszt csak az argumentumdaraboláshoz van. |
| `go vet ./...` | sikeres | Nem jelzett problémát. |
| `go test -race ./session ./mcp ./updater` | sikeres | A diktálási csomagnak nincs tesztje, így a fenti race-eket ez nem gyakorolja. |
| `npm run build` | sikeres | A11y/unused CSS és nagy, kb. 955 KiB-os JS chunk warningokkal. |
| `npm run check` | **sikertelen** | 14 error, 39 warning, 38 hint. |
| Go coverage | nagyon alacsony | root/dictation/MCP/updater/filters: 0%; session: 1,2%. |

A típusellenőrzési hibák fő csoportjai:

- hiányzó `bufferSendEnter` settings mező;
- hibás form-submit handler típus;
- backend/frontend Task és Subtask modellek eltérése;
- `WindowInfo` mockból hiányzó mezők;
- xterm theme API eltérés (`selection`);
- `Select` események sima `string` típusa a szűk union típusok helyett;
- a TypeScript target/library miatt nem ismert `style.contentVisibility`.

## Tesztelési javaslatok

Első körben a következő regressziós tesztek adnák a legtöbb értéket:

1. `StreamingRecognizer.Stop` és sok párhuzamos `SendAudio`, race detectorral.
2. Audio capture producer/consumer ownership és chunk-immutabilitás.
3. Notes: gépelés → azonnali tabváltás → mindkét tab tartalma helyes marad.
4. New Session: Enter pontosan egy backend hívást indít; konfliktus esetén nem indul létrehozás.
5. MCP 64 KiB feletti válasz, child exit és automatikus recovery.
6. Semver összehasonlítás és updater checksum mismatch.
7. Sessionváltás közbeni out-of-order Task/Diff/Preview válaszok.
8. Projekt-ID containment és rosszindulatú `../`/`/` nevekkel végzett törlési tesztek.

## Pozitív megfigyelések

- A terminál WebSocket per-launch tokent és origin ellenőrzést használ; a reconnect map-cleanup védi az új kapcsolatot a régi cleanupjától.
- A frontend dinamikus szövege jellemzően Svelte text interpolationnel jelenik meg. A keresési kiemelésnél használt `{@html}` előtt HTML- és regex-escape történik; igazolható XSS-t nem találtunk.
- A session adatok egy része temp fájl + rename mintával atomikusan íródik.
- A production frontend build és a Go build/test útvonal jelenleg működik.

## Összegzés

A legsürgősebb problémák nem stílusbeli eltérések, hanem reprodukálható correctness és adatbiztonsági hibák. A diktálási konkurenciakezelés, a Notes célazonosító-kezelése és a dupla form submit javítása után az MCP/updater robusztusság, majd a frontend request identity és type-check rendbetétele következzen. Az alacsony tesztlefedettség miatt minden javítást célzott regressziós teszttel érdemes lezárni.
