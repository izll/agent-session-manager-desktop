# Kapcsolódó mappák a fülhöz – megvalósítási terv

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Cél:** egy fülhöz tetszőleges mappák kapcsolhatók. Ezek diffje és fájljai egy ablakban nézhetők, amelyet a fejléc jobb oldali ikongombjai közül az első nyit meg. A diff saját mappája fülenként is választható, és szerveren is.

**Architektúra:**
- A fül rekordja (`FollowedWindow`) tárolja a kapcsolt mappákat; a munkamenet saját ablakáé az `Instance`-en van.
- A diff és a fájl API-k a fül mappája és a diff-mappa mellett a kapcsolt mappákat is elfogadják gyökérként (`validateRootSnapshot`). Így a meglévő Diff és FileBrowser komponens egy rögzített gyökérrel újrahasználható az ablakban.

**Spec:** a user kérései (2026-11-13):
- „lehessen tetszőleges mappában diffet nézni”
- „meg lehessen kapcsolódó mappát felvenni a fülhöz hogy diffet lehessen nézni meg fájlt tallózni, ezeket jó lenne tudni dialógusban is megnézni, így jó lenne a terminál diff stb headerből elérni gombbal. A jobb oldali gomboknál ikonos gomb kellene tooltippel (mentési pontok diff felül) és ezek elejére kellene a diffes gomb”

## Global Constraints
- A mappát azon a gépen kell ellenőrizni, ahol a fül fut: helyben `os.Stat`, szerveren `test -d`, a tesztekben az `App.remoteFolderExists` cserélhető.
- Gyökérként csak a fül mappája, a diff-mappa vagy a fül egyik kapcsolt mappája fogadható el.
- Minden új szöveg mind a 20 nyelven.
- Commit csak a user kérésére.

## Review Focus
1. **Egy kapcsolt mappát közben töröltek.** A lista mutassa, a diff adjon hibaüzenetet, a fül ne törjön el.
2. **Szerveres fül.** A szerveres útvonal gyökérként is egyezzen (a `sameRoot` helyi symlink-feloldása ott nem megy).
3. **Ugyanaz a mappa kétszer.** Ne kerüljön be kétszer.
4. **Fül bezárása vagy áthelyezése.** A kapcsolt mappák a fül rekordjával együtt mozognak.
5. **Az ablak nyitva van, és közben a fül vagy a munkamenet vált.** Az ablak a megnyitáskori fülhöz kötődik.

### Task 1: Fülenkénti és szerveres diff-mappa (kész)
`SetDiffDir`, a `DiffFolder.Scope`/`ServerID`, `checkFolderOn`; tesztek a `diff_folder_test.go`-ban.

### Task 2: Kapcsolt mappák a háttérben
- `FollowedWindow.LinkedDirs`, `Instance.LinkedDirs` (a munkamenet saját ablakáé), `LinkedDirsFor(idx)`, `SetLinkedDirs(idx, dirs)`
- API: `GetTabLinkedFolders(id, idx) []LinkedFolder{Path, IsGitRepo, Missing}`, `AddTabLinkedFolder(id, idx, dir, project)`, `RemoveTabLinkedFolder(id, idx, dir, project)`
- `browseInstance` → `inst.LinkedRoots`; `validateRootSnapshot` ezeket is elfogadja; a `sameRoot` egy helyben nem létező útvonalnál pontos egyezést vár.
- Tesztek: hozzáadás, ismétlés, nem létező mappa, eltávolítás; egy kapcsolt gyökér diff-listája; egy idegen gyökér elutasítása; szerveres útvonal egyezése.

### Task 3: Az ablak és a fejléc
- `LinkedFoldersDialog.svelte`: balra a mappák (hozzáadás helyben vagy `RemoteDirPicker`-rel, eltávolítás), jobbra Diff | Fájlok. Ehhez a Diff és a FileBrowser kap egy `fixedRoot` propot.
- Fejléc jobb oldal: az új gomb elöl, a Mentési pontok és a Diff felül ikongomb tooltippel.
- A diff mappa-menüje a kapcsolt mappákat is felkínálja.
- Böngészős teszt fixture-rel.
