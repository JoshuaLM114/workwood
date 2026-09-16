# workwood history-cleanup procedure

Status: local history rewrite completed on 2026-09-15 after explicit authorization.
Remote publication remains a separate approval. The procedure below records the
original plan; the execution result takes precedence where the steps differ.

## Execution result

- Local `main`: `fef32cfa09574a3866445de4e0752411259d3153`.
- Three commits rewritten; all 19 commit trees checked against their originals.
  Only the identified test-fixture value changes in history. Authors, commit
  messages and both local tags are preserved.
- Final local scan: 466 Git objects and 70 working files, zero matches for the
  audited company/project identifiers and paths. Old objects and reflogs are
  removed from the active checkout.
- Existing working files are preserved byte-for-byte during application. The
  current edits remain unstaged; no additional cleanup commit is created. This
  status update is the only subsequent working-file change.
- Tests, vet, dependency tidy and Git integrity checks pass. The standard build
  succeeds with a sandbox module-cache write warning; a second build with
  `-buildvcs=false` succeeds without that warning. Dependency files are unchanged.
- Remote `main` is rechecked at
  `d1992f252387d4561c5a27566976274cc9313bca`. The guarded force-push dry-run passes;
  no remote update is executed. Local remote-tracking refs contain the rewritten
  IDs, so publication must use the explicit original remote ID in its lease.
- Private backup and verification records:
  `/private/tmp/workwood-history-0biu8_jh/`. The original backup deliberately retains
  the old history outside this repository. Forks and hosting-side copies remain
  outside this local cleanup.

## Objective and scope

Remove the identified unrelated company and its project references from workwood's
history while retaining package identifiers, Charm dependencies, required
attribution, and workwood's installation/update links. Historical rewriting is
limited to the confirmed fixture; generic examples are not rewritten further.

Keep the removed names and replacement-rule inputs outside the repository. This
procedure deliberately identifies findings by commit and file rather than
reintroducing the unwanted text. The existing cleanup of 16 tracked files and
ignored `notes.md` must survive the history cleanup.

## Verified starting point

These observations are a snapshot; repeat the inventory before execution.

| Item | Observed state |
| --- | --- |
| Local `main` and remote `main` | `d1992f252387d4561c5a27566976274cc9313bca` |
| Remote | `git@github.com:JoshuaLM114/workwood.git` |
| Reachable local history | 19 commits |
| Known unwanted reference | One test fixture, present in three commits and two distinct blobs |
| First affected commit | `bb539332ad17573bcf607f311985264645e5e276`, `superfeature/safety_test.go` |
| Next affected commit | `fa654899c48617a814c189aaf87eba8031fe232d`, same path |
| Current committed path | `d1992f252387d4561c5a27566976274cc9313bca`, `superfeature/superfeature_test.go` |
| Clean replacement | `sample-db` for both the test-case label and input |
| Local-only tags | `v0.0.1` and `backup-before-scrub`, both at `21d5b8c6d823ec13160e0cb67a10b9ff72f9ced1` |
| Advertised remote refs | Only `refs/heads/main`; no advertised tags or pull-request refs |
| Hosting state | Private repository, one fork, zero pull requests and zero releases returned by the API |
| Previous rewrite | Existing local `.git/filter-repo/` records; inspect separately from this cleanup |
| Working tree | 16 modified tracked files; ignored notes already generalized |

The known-name scan covered all 466 local Git objects, including objects outside
current refs. It found two matching blobs, both reachable. It found no matching
commit/tag objects or additional matching unreachable objects. Historical URL
review found workwood links and generic example URLs. This does not establish the
state of another clone, the fork, or hosting-side cached objects.

## 1. Preserve work and establish the execution boundary

1. Joshua explicitly authorizes a local rehearsal: creating isolated clones,
   rewriting their history, and making a local cleanup commit in the validation
   checkout. Remote publication remains a separate approval.
2. Coordinate a pause in repository writes with collaborators. The operator does
   not send messages on Joshua's behalf without separate permission.
3. Choose a private audit directory outside this repository. Restrict access;
   original snapshots, patches, backups and rules can contain removed text.
4. Record current status, full local/remote ref inventories and object IDs. Save a
   complete copy of the original checkout, including `.git`, untracked files and
   ignored files, in that private directory. A Git bundle alone would omit local
   working files and some non-ref state. Keep this backup offline and never push it.
5. Separately snapshot the current contents and names of the 16 cleaned tracked
   files. Preserve ignored `notes.md` separately; it is not part of a Git commit.
   Keep this procedure outside the cleanup commit unless Joshua requests it.
6. Immediately before cloning, repeat the remote inventory, fork/PR/release
   checks, branch-protection check, and local status. If `main` or the local files
   changed, reconcile those changes before proceeding. Record the live remote
   `main` object ID as `WORKWOOD_EXPECTED_MAIN` for publication.

## 2. Prepare and review the exact rules

Recover the original test fixture from the first affected commit listed above and
write its exact literal replacement to a private `replacements.txt` file. The
replacement is `sample-db`. Do not put the removed value in a tracked script,
commit message, issue, or this document.

Review historical content, filenames, ref names, commit/tag messages and author
metadata against the agreed scope. Keep actual dependencies and project-owner
metadata. Extend the private rules only for confirmed unwanted references. A
literal match avoids altering unrelated identifiers that contain similar letters.

Replacements use `literal:OLD_VALUE==>NEW_VALUE` syntax. `--replace-text` processes
file contents; `--replace-message` handles commit/tag messages. Renaming files or
refs requires separate rules if the audit discovers affected names. Rewriting all
history handles both test-file paths without removing the tests.
[git-filter-repo manual](https://github.com/newren/git-filter-repo/blob/main/Documentation/git-filter-repo.txt)

## 3. Rehearse in an isolated clone

The following commands are procedure examples, not instructions to execute during
review. Set `WORKWOOD_AUDIT_DIR` to the approved private directory first.

```sh
WORKWOOD_REMOTE='git@github.com:JoshuaLM114/workwood.git'
git clone --mirror "$WORKWOOD_REMOTE" "$WORKWOOD_AUDIT_DIR/rewrite.git"
git -C "$WORKWOOD_AUDIT_DIR/rewrite.git" filter-repo \
  --sensitive-data-removal \
  --replace-text "$WORKWOOD_AUDIT_DIR/replacements.txt" \
  --replace-message "$WORKWOOD_AUDIT_DIR/replacements.txt"
git clone --no-local "$WORKWOOD_AUDIT_DIR/rewrite.git" \
  "$WORKWOOD_AUDIT_DIR/verify"
```

Use the fresh-clone check; do not bypass it with `--force`. The sensitive-data
mode fetches additional refs and produces cleanup records. Inspect its ref and
commit maps and first-changed-commit report. Save the reports privately.
[git-filter-repo manual](https://github.com/newren/git-filter-repo/blob/main/Documentation/git-filter-repo.txt)

The remote currently has no tags. Keep the two existing local-only tags offline
until their ancestry is verified; the known unwanted fixture first appears after
their target commit. They need no remote publication. If further findings affect
them, include their histories in a revised rehearsal before restoring them locally.

In the validation checkout:

1. Compare rewritten `main` with its original tree. For the currently known
   finding, only the fixture strings should differ.
2. Overlay the preserved current contents of the 16 cleaned tracked files. Review
   the resulting diff before staging the explicit file list and creating the
   separately authorized cleanup commit. Copying the reviewed final contents
   avoids replaying a patch whose old fixture line was already rewritten.
3. Preserve all existing author information. Use Joshua's configured identity for
   the new cleanup commit and include no AI attribution.
4. Record the resulting final `main` object ID and include it in the publication
   review. This is the candidate to publish, including both historical removal
   and the current-file cleanup.

## 4. Validation and acceptance criteria

Before requesting publication approval:

- Scan every reachable commit tree for the private forbidden terms. Inspect
  filenames, ref names, full commit/tag messages and metadata too. Scan binary
  blobs separately; do not assume text replacement sanitizes generated binaries.
- Inspect all objects in the isolated rewrite and validation repositories, not
  just their current checkouts. Investigate leftover matching objects and refs.
- Verify the known fixture is absent throughout history and both renamed test
  paths retain their tests. Review the full before/after commit and ref maps.
- Verify no backup, original-history, or replacement ref will be published.
- Verify the candidate tree matches the approved cleaned files and dependency
  metadata remains unchanged.
- In the validation checkout, run `go mod tidy`, reconcile any deltas, then run
  `go test ./...`, `go vet ./...`, `go build -o bin/workwood .`, `git diff --check`,
  shell syntax checks and JSON parsing for both locale files. Use the project's
  toolchain and a writable cache. Existing local checks passed before rewriting;
  that does not validate the future rewritten candidate.
- Check CI for the candidate if available. No CI runs were returned for `main`
  during the current-file cleanup; do not describe that as passing CI.

Report exact changed refs, old/new object IDs, scan counts, test results and any
unresolved external copies. A zero-match search establishes absence of the audited
terms in the scanned objects, not universal deletion from other systems.

## 5. Publish only after explicit approval

Present the final candidate and exact force-push operation to Joshua. For the
currently observed remote inventory, publish only `main` from the validation
checkout. Recheck the entire remote ref inventory first; any new or changed ref
requires review before publication.

```sh
git -C "$WORKWOOD_AUDIT_DIR/verify" push --dry-run \
  --force-with-lease="refs/heads/main:$WORKWOOD_EXPECTED_MAIN" \
  "$WORKWOOD_REMOTE" refs/heads/main:refs/heads/main

# Execute only after Joshua explicitly authorizes remote publication.
git -C "$WORKWOOD_AUDIT_DIR/verify" push \
  --force-with-lease="refs/heads/main:$WORKWOOD_EXPECTED_MAIN" \
  "$WORKWOOD_REMOTE" refs/heads/main:refs/heads/main
```

The explicit expected object ID makes the update fail if remote `main` has moved.
A rejected lease requires a new inventory and rehearsal; do not override it.
Avoid a blanket mirror push, which can update or delete unrelated refs. Any
required branch-protection change also needs explicit approval and restoration.
[Git push documentation](https://git-scm.com/docs/git-push)

If new affected tags or branches appear before execution, prepare an explicit
old/new ref table and separately approve their treatment. Do not silently move a
published release tag. Changing content for an existing Go module version can
conflict with stored checksums; use a new version for future releases and account
for existing cached copies. No remote release/tag was observed in this inventory.
[Go module authentication](https://go.dev/ref/mod#authenticating)

## 6. Verify publication and clean remaining copies

1. Fetch a completely fresh clone from the host and repeat the all-history scan;
   verify remote `main` matches the approved object ID. Recheck remote refs and CI.
2. Joshua coordinates verification and cleanup of the one observed fork. This
   procedure does not authorize modifying the fork or contacting its owner.
3. Existing collaborators and other machines use fresh clones. Preserve and
   review any uncommitted work before migration. Do not merge old history back
   into the cleaned repository.
4. Inspect hosting-side references and caches, including references associated
   with the earlier rewrite. A force-push cannot guarantee their removal. If
   eligible sensitive material remains accessible, Joshua can request hosting
   support; support does not promise removal of non-sensitive data. Forks and
   other people's clones need their owners' participation.
   [Hosting cleanup guidance](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/removing-sensitive-data-from-a-repository)
5. Replace the original working checkout only after verifying every local file is
   preserved. Keep its old `.git` and private backup quarantined until Joshua
   authorizes disposal. A clean active checkout does not erase those copies.
6. Review module caches, saved archives and generated artifacts if they contain
   affected source. Record what was inspected and what remains outside our control.

## Recovery and completion

Before publication, a failed rehearsal leaves the original checkout and remote
intact. Start another isolated rehearsal from the preserved baseline.

After publication, restoring old refs would restore the removed history. Prefer
fixing the sanitized candidate; restoring old remote refs requires a new explicit
decision. Keep the backup offline until validation and collaborator migration are
complete, then agree on its disposal.

Completion report must distinguish: cleaned remote refs, verified fresh clones,
fork status, host-cache status, and retained private backups. Do not claim that
every copy is erased while an item remains unverified.

## 👉 **ACTIONS FROM YOU** 👈

👉 Authorize the guarded force-push of local `main` if the remote history should
also be replaced. The local rewrite and validation are complete.

👉 Coordinate a write pause and the fork owner's participation before publication.

👉 Approve old-clone and backup disposal only after migration is verified.
