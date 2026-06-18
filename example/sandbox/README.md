# workwood sandbox

A self-contained playground for trying `workwood` without touching real GitHub or
your real `~/.workwood`. Everything (an isolated home, sample repos with local git
origins, a team super-repo, and a copy of the binary) lands in one directory you
can `rm -rf` when done.

## Two ways to make one

**From an installed binary** (preferred once `go install .` has been run):

```sh
workwood sandbox ~/workwood-sandbox
source ~/workwood-sandbox/activate
```

**Straight from this checkout** (no install needed — builds first):

```sh
example/sandbox/run.sh                 # creates ./workwood-sandbox
# or: example/sandbox/run.sh /tmp/ww-play
source ./workwood-sandbox/activate
```

`source .../activate` sets `WORKWOOD_HOME` to the sandbox and puts the bundled
`workwood` on your PATH. The project `sandbox` is registered and default.

## What you get

- Sample repos `api`, `web`, `worker` — real git repos with their own local
  `origins/`, each carrying a `.workwood/` folder (`up`, `ssh`, `helloworld` children).
- A team super-repo (`super/`) with a committed sample feature `welcome` and a
  **project-local plugin** `context` that prints the resolved context (no tmux
  needed). The sample repos also ship `.workwood/{up,ssh,helloworld}` children.
- The global `helloworld` + `tmux` + `ssh` plugins seeded into the sandbox's home.

The generated sandbox's own `README.md` is a full step-by-step walkthrough.

```sh
workwood sf list                       # the shared 'welcome' feature
workwood compose helloworld welcome    # parent 'hello ' + child 'world'
workwood compose context welcome       # dump the resolved context
workwood sf create play "playground"
workwood sf add play api feature/x
workwood sf target add play api deploy # additive custom target
workwood compose tmux play             # needs tmux
workwood                               # the TUI
```

Clean up with `rm -rf <sandbox-dir>`.
