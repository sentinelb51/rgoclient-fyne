# rgoclient-fyne

[Fyne](https://github.com/fyne-io/fyne) with four patches for
[rgoclient](https://github.com/sentinelb51/rgoclient). Not a general-purpose
fork and not intended for anyone else's use — if one of these ever lands
upstream, the patch here goes away rather than staying as a second answer.

Every change is under Fyne's `internal/`, which is why the fork exists at all:
none of it can be reached from an importing module. See
[PATCHES.md](PATCHES.md) for what each one does and why.

The module path is still `fyne.io/fyne/v2`, deliberately — the fork is consumed
through a `replace`, and Go accepts a replacement whose `go.mod` declares either
the replacement path or the path being replaced. Leaving it alone means every
import inside the tree still resolves, so this repo builds and tests standalone
exactly like upstream does.

## Using it

```
replace fyne.io/fyne/v2 => github.com/sentinelb51/rgoclient-fyne/v2 v2.8.0-rgo.1
```

Nothing else: no submodule, no vendored tree, no checkout step in CI.

## Layout

| branch | what it is |
|---|---|
| `upstream` | pristine Fyne, one commit per release, tagged `upstream/vX.Y.Z` |
| `main` | `upstream` plus one commit per patch, tagged `vX.Y.Z-rgo.N` |

Keeping upstream on its own branch is what makes a bump a rebase: the patches
are replayed with real three-way context, so anything upstream has moved shows
up as an ordinary merge conflict rather than as a patch that applied cleanly to
the wrong place.

## Updating Fyne

```
./update-fyne.sh v2.9.0     # bump, verify, tag; does not push
./update-fyne.sh            # verify the tree against its upstream base
```

It refuses a dirty tree, replaces the `upstream` branch with the new release,
rebases the patch commits onto it, and checks that the same number of
`RGOClient patch` markers came out the other side. That last check is the one
that matters: a dropped vsync or font-cache patch still compiles and still
runs, so a passing build is not evidence a patch survived.

On a conflict it stops mid-rebase and says what to do. What it cannot decide is
what a patch should mean once upstream has rewritten the code underneath it.

## Two things that will bite

**Never move a published `-rgo.N` tag.** The Go checksum database records what a
tag contained the first time anyone fetched it, so re-pointing one turns every
later fetch into a `SECURITY ERROR` about a checksum mismatch — including for
builds that were working. Cut the next N instead; `update-fyne.sh` already picks
an unused one.

**Clone with long paths enabled on Windows.** Some of Fyne's `testdata`
filenames run past 110 characters, which is enough to break `MAX_PATH` in a
deep directory:

```
git clone -c core.longpaths=true https://github.com/sentinelb51/rgoclient-fyne.git
```

This only affects working on the fork. Consuming it does not: `go` fetches the
module as a zip from the proxy and never clones it.
