# The chapter tag on a side commit: the proof

Decision 5: the tag `chNN` on a side commit only after it is proved in a clone. It is proved, and **not** adopted: `chapter-v4.js` still inserts the prose below any later chapter, and this waits for Álvaro's word, because it changes spec 11.1 and 11.2.

`prove-side-commit.sh <new-dir>` replays chapters 9 and 10, their real sessions and prose, in a clone with a bare remote. Chapter 10's sessions are already on main when chapter 9 lands, the case that moves tags today. About 4 min. Run on 2026-11-04 at the commit that adds this file: every check held.

```
main:  … ch09-s3 ── ch10-s1 ── … ── ch10-s4 ── land ch09 ── land ch10
            └── ch09 (prose, list, session lines)   └── ch10 (prose, list, session lines)
```

| Check | Result |
|---|---|
| Chapter 9 lands; a `ch10-s*` tag moves | none: no rebase, no retag |
| `ch09` and `ch10` on main | no; `ch09`'s parent is `ch09-s3` |
| Pushing each finished chapter | ordinary pushes, no force |
| `ci/check-tags.sh ch09 'ch10-s*' ch10` | all pass, though `ch10`'s tree holds no chapter 9 prose |
| `bookx verify` at main | OK |
| `bookx web`, published and `--all` | both chapters, cards from their tags; **pages byte-identical** to the real history's |
| The reader's `git fetch ref 'refs/tags/*:refs/tags/ref/*'` | brings both tags with their prose |
| `ci/retag-all.sh` today | refuses: «ch09: want exactly one commit on main whose subject is 'book: chapter 09 prose', found 0», moves nothing |
| `ci/retag-all.sh` with two lines | runs, moves nothing, a milestone on a side commit stays with it |

## What adopting it would change

- `ci/retag-all.sh`: a chapter tag or a milestone whose commit is not on main is left alone. One line in each loop; the script prints the diff.
- `chapter-v4.js`, `integrate`: tag on the prose branch's tip, then one forward commit `book: land chNN` on main with the chapter, its list and the `verified-tags` lines; no cherry-pick into the past, no `rebase --onto`, and the frozen patch-ids hold as SHAs. `verified-tags.txt` merges by added lines.
- Spec 11.1 and 11.2, and the roadmap's line on tags.

## What it does not show

- `git log main` no longer shows `chNN`, and `git tag --merged main` leaves it out: `ci/check-tags.sh 'ch*'` at each Part's close is what catches a tag gone wrong.
- A chapter tag's tree lacks the prose of the chapter before it when they overlap. Nothing here reads it, but a future check that walks the book inside a tag would see a hole.
- It replays two small chapters of Part I, with no fix after the landing. An unfreeze before publishing happens before the tag exists, so it should not change; nobody has run it.
