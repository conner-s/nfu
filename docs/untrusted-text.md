# Untrusted text: every path to the terminal

nfu shows text it did not write: the fields of a `flake.lock`, project files,
paths, what nix, git, nh and deploy print, and what the user typed or passed.
Whoever wrote a flake chose its input names and pins, a project file can come
from anyone's repository, and an error quotes a remote server. A terminal acts
on control sequences wherever they appear, and bubbletea 2.1 replays some of
them from a view's text, so all of it is cleaned by the `termsafe` module
(`github.com/conner-s/termsafe`) before it is drawn or
printed.

This page traces each path from where the text comes from to where it is
shown, and lists the places that get the raw value on purpose. Check a new
path against it, and add the path here.

## The rules

1. Whatever draws takes a `termsafe.Text`, never a `string`, for anything nfu
   did not write: the row types and render helpers of `internal/ui`, the
   manager's messages, steps, rows and toast, `project.PlanText`. Only
   cleaning makes a `Text`, so the compiler refuses a raw string there.
2. A raw string is cleaned by one of the functions in `internal/ui/text.go`
   and `internal/ui/notice.go`, or by `proc.RunLines` for the output of nh and
   deploy. The tables below name the one that does it on each path.
3. The policy depends on the kind of text:
   - Names, pins, paths, descriptions, commit subjects and errors are text a
     person acts on. A bidirectional control or an invisible character in them
     is shown as a marker such as `<U+202E>` (`termsafe.Mark`).
   - The output of nh and deploy is a log that is watched going by. There they
     are removed (`termsafe.Drop`).
   - Text typed or pasted into a field is cleaned and then used, so they are
     removed there too (`termsafe.Drop`): a marker would become part of the
     host name or the argument.
   Control sequences, control characters and invalid bytes are removed the
   same way under both.
4. Everything that is not display keeps the raw value: see the last table.
5. When cleaning changes something the user typed or passed, or when a name is
   not what it looks like, nfu says so: see "What the user is told".

The domain types (`flake.Input`, `flake.UpdateResult`, `project.Entry`,
`project.Plan`, `config.Project`) are read, compared, written and handed to
nix, and are not changed. A view builds its display rows from them at its top
(`ui.InputRows`, `ui.ResultRows`, `rowFor`, `stepsForPlan`, `Plan.Text`) and
draws only the rows. Messages that carry text to draw (`runLineMsg`,
`updateFinishedMsg`, the toast, a probe's error) hold a `Text` from the moment
they are built.

## TUI: the picker (bare `nfu`)

| Text | Comes from | Cleaned at | Drawn at |
| --- | --- | --- | --- |
| Input name, type, ref, pin | `flake.lock`, through `nix flake metadata --json` | `ui.NewInputRow` (`ui.Name`, `ui.Line`, `ui.Pin`) | every row; the list left on screen after a cancel |
| Why a name deserves a second look | the same name | `ui.InputNameNote` | `! …` after the row |
| Flake path and its last element | config `flakeRoot`, `--flake`, `$NFU_FLAKE`, `$FLAKE` | `ui.Line` in `header` | title line |
| Upstream pin | the lock nix writes for a probe (`nix flake update --output-lock-file <file> -- <name>`) | `ui.Pin`, when the `upstreamMsg` lands | `→ <pin>` after the row |
| Probe error | nix's stderr, which quotes the remote and, for git inputs, git and ssh | `ui.ErrLine`, when the `upstreamMsg` lands | `! …` after the row |
| Names being updated | the selected inputs | `ui.Names`, when enter is pressed | `updating …` |
| Updated input, old and new pin | `flake.lock` before and after | `ui.NewResultRow` | result rows |
| Commit subject | input names and pins (`lock.Message`) | `ui.Line` in `finished` | `committed flake.lock: …` |
| Commit warning | git's stderr | `ui.Line` in `finished` | `! …` |
| Error of a failed load or update | nix's stderr, whole | `printError` in `main.go`, after the TUI has closed | stderr |

## TUI: the project manager (`nfu project`)

| Text | Comes from | Cleaned at | Drawn at |
| --- | --- | --- | --- |
| Project name | the file name in the projects directory, or a key of `[projects]` in `config.toml` | `rowFor` (`ui.Name`) | list, detail title, TOML pane title |
| Description, flake root, inputs, includes, hosts | project files, `config.toml`, the index `projects.toml` | `rowFor` (`ui.Line`, `ui.Names`), `targetLabel` | list columns, detail meta box |
| What the last run did | `runs.toml` | `rowFor` (`ui.Line`) | detail meta box |
| Projects directory | config `projectsDir` | `ui.Line` in `viewHeader` | header |
| Current project | the `current` marker file | `ui.Name` in `viewHeader` | header |
| Step titles, details and commands | the plan: inputs, hosts and their deploy arguments, the flake path | `stepsForPlan` (`ui.Names`, `ui.Linef`) | detail timeline, run sidebar and header, dry-run log |
| Hosts in the plan that are not what they look like | the same hosts | `Plan.Notes` (`ui.HostNameNote`) | `! …` under the timeline |
| Why a project gets no plan | `project.BuildPlan`, which quotes with `%q` the input names nix cannot update | `ui.Linef`; `sayErr` when `r` or `d` is pressed | `✗ …` in the detail view; toast |
| Why a project will not resolve | `project.Resolve`, which quotes project names with `%q` | `ui.Linef` | `✗ …` in the detail view |
| The project as TOML | the project, marshalled again | `tomlLines` (`termsafe.Mark.Clean`, then per line) | TOML pane |
| Log of a run: update results | `flake.lock` before and after | `ui.Linef`, `ui.Pin` in `runEngine` | run log |
| Log of a run: commit subject and warning | input names and pins; git's stderr | `ui.Linef` in `runEngine` | run log |
| Log of a run: nh and deploy | both streams of the child, read by `proc.RunLines`, which ends a line at `\n`, `\r\n` or a lone `\r` | `proc.RunLines` (`termsafe.Drop.LineReport`) | run log |
| Log of a run: the error of a failed step | nix's stderr, whole; the exit status of nh or deploy | `failLines` (`ui.ErrLines`) | run log, one pane line per line of the error |
| Toast | nfu's own words, a project name, the first line of an error | `ui.Words`, `ui.Linef`, `sayErr` (`ui.ErrLine`) | footer; on lines of its own when it does not fit |
| Filter | typed text | `ui.Typed` in `keyList`, as it is typed | filter line, "no projects match" |

## TUI: the project form (`nfu project add`, `edit`, and `e` / `n` in the manager)

| Text | Comes from | Cleaned at | Drawn at |
| --- | --- | --- | --- |
| Input names | `flake.lock`; inputs the stored project names that the flake no longer has | `ui.NewInputRow` in `viewInputs` | input list, with `! …` for a name that deserves a second look |
| Host names and arguments | the host catalog (`config.toml`, `projects.toml`), the project's own `hostDefs` | `ui.Name`, `ui.Names`, `ui.HostNameNote` in `viewHosts` | host list |
| Name of the project being edited | the project's file name | `ui.Name` in `viewName`. It is kept beside the text field, not in it. | `name (fixed)` |
| Name given on the command line | `nfu project add <name>` | `ui.Typed` in `newFormModel` | name field |
| Typed and pasted text | `tea.KeyPressMsg`, `tea.PasteMsg` | `ui.Typed` in `typed` and `updatePaste`, before the field gets it | name, host name and host argument fields |
| A host's stored arguments, opened with `e` | the host catalog | `ui.Typed` in `updateHosts` | argument field |
| The host's name beside them | the host catalog | `ui.Name` | name field, shown only |
| Warnings | nfu's own words and counts | built as a `Text` | above the footer, until the form closes |

## Plain output (every command off the TUI)

| Text | Comes from | Cleaned at | Printed to |
| --- | --- | --- | --- |
| The last error, `nfu: …` | anything: nix's stderr whole, git's, a `flake.lock` name, a path, a TOML parse error, an argument | `printError` (`ui.Err`) | stderr |
| A flag error, before the usage text | the argument the flag parser could not read | `printError` | stderr |
| Title line | the flake path | `titleLine` (`ui.Line`) | stdout |
| `nfu list`, `--dry-run`: input rows | `flake.lock` | `ui.InputRows` | stdout |
| `nfu <input>`: spinner label, result and failed rows | input names from the command line and the lock, pins | `ui.Name`, `ui.NewResultRow` in `streamReporter` | stdout |
| `would run nix flake update …` | input names | `ui.Linef`, `ui.Names` | stdout |
| `committed flake.lock: …`, `! …` | commit subject; git's stderr | `ui.Line` in `streamUpdate` and `project.Run` | stdout, stderr |
| `nfu project run`: the plan, the step headers | the plan | `Plan.Text` | stdout |
| `nfu project run`: the closing summary | project name, hosts | `runSummary` (`ui.Name`, `ui.Line`) | stdout |
| `nfu project list` | project files; the reason one will not resolve | `renderProjectList` (`ui.Name`, `ui.Names`, `ui.Linef`) | stdout |
| `nfu project use` | the `current` marker file; the name given | `ui.Name` | stdout |
| `saved project:`, `removed project:` | the name given, the file's path | `ui.Name`, `ui.Line` | stdout |
| `commit …?`, `committed: …`, `! commit failed: …` | file paths and the repository's directory name; git's stderr | `ui.Linef`, `ui.Line` in `offerCommit` | stdout, stderr |
| Usage, prompts, hints | nfu | own text | stdout, stderr |

## What nfu does not stand in front of

| What | Can outside text reach the terminal uncleaned? |
| --- | --- |
| `nh os switch` and `deploy` under `nfu project run` (`proc.Stream`) | Yes. They are given the terminal, so that progress streams and a sudo prompt works. Whatever they write, build logs and remote output included, goes straight to it. Under the manager the same commands run with their output piped, and that is cleaned. |
| `sudo -v` before a run in the manager (`tea.ExecProcess`) | Yes, while it has the terminal. |
| `$EDITOR` on a project file, `E` in the manager (`tea.ExecProcess`) | Yes: the editor draws the file itself. |
| nix and git | No. nfu captures both streams of every nix and git command; what they said reaches the screen only through an error or a warning, cleaned. |
| `--json` | It is data for another program and carries every value as it is. `encoding/json` escapes the C0 controls, ESC among them, so no escape sequence leaves this way. A C1 control, a bidirectional control or an invisible character in a name does. |
| `nfu project add --print` | It is the contents of a file. The TOML encoder escapes the C0 controls in a string; anything else a value holds is in the output. |
| A panic | Not by any path found. nfu does not panic itself. bubbletea restores the terminal and prints the panic value and a stack trace to stderr uncleaned; a runtime panic's message holds indices and type names, not data. |
| bubbletea's and lipgloss's own control sequences, the plain spinner's `\r` and erase-line | Own. |

Two views draw text an encoder has already escaped: the TOML pane, and an
error that quotes a name with `%q`. An ESC in a value appears there as the
printable `\u001B` or `\x1b`, with whatever followed it as text. That is what
the value holds, readable and inert, and it is left that way.

## What the user is told

| When | TUI | Plain output |
| --- | --- | --- |
| An argument or a flag's value holds a control sequence, a control character, a bidirectional control or an invisible character | The same line, printed before the TUI opens | `nfu: WARNING: An argument holds … It is shown as "…" and used as it was given. …` on stderr |
| Text typed or pasted into a field, a name given to `nfu project add`, or a host's stored arguments opened for editing held any of those. They were removed and the cleaned text is used. | `WARNING: … nfu removed that and uses the cleaned text. …` above the form's footer until the form closes; for the manager's filter, as a toast | not applicable |
| A flake input's name is not one nix will update: anything but ASCII letters, digits, `_` and `-`, starting with a letter. nfu will not pass it to nix. The note says what cleaning found in it and names any word that mixes scripts, by code point. | `! …` after the row in the picker and the form. The picker shows `[!]` where the checkbox would be and will not select or probe the row; the form will not tick it. A project that names one gets no plan: `✗ … is not a name nix can update …` in the detail view and as a toast on `r` and `d`. | `! …` after the row in `nfu list` and in the row of `nfu project list`. Asked to update one, to preview it or to run a project that names one: `nfu: "…" is not a name nix can update: …`, exit 1, before nix is run. `nfu all` updates the others and ends with `nfu: not updated: …`. |
| A deploy host's name holds something cleaning removes or marks, has a word that mixes scripts, or is not plain ASCII | `! …` after the row in the form; under the plan in the manager | under the plan of `nfu project run`; in the row of `nfu project list` |
| Text from nix, nh or deploy had an unterminated control string. It was cut at the end of its line, and the rest of that line went with it. | An `info:` line under it in the run log | `nfu: info: …` under the error |
| Text from a file or a tool held complete control sequences or control characters | Nothing: they are removed. In a name, the note above says so. | Nothing |
| Text from a file or a tool held bidirectional controls or invisible characters | Each is shown as a marker such as `<U+202E>`; in the log of nh and deploy they are removed | The same |

A notice counts what was removed and never draws it.

The two name rules differ on purpose. A flake input's name has a registry
rule: `nix flake update <name>` takes only `[a-zA-Z][a-zA-Z0-9_-]*`
(`flakeIdRegexS` in nix, the same from 2.19 to 2.35), though a `flake.lock`
can hold any string: nix itself locks an input written as
`inputs."--refresh"`. So nfu holds input names to that rule
(`flake.IsInputName`), which also catches a name written wholly in another
script, and refuses to hand nix a name that fails it: such a name can only be
an input nix would refuse, or an option. A deploy host is an attribute of the
flake's `deploy.nodes` and may be any string, so nothing is required of it;
nfu notes a word that mixes scripts and, short of that, any name that is not
plain ASCII, because the mixed-script check cannot see a name written wholly
in look-alike letters. The host note changes nothing nfu does with the name.
Project names are not noted: `nfu project add` and the form accept only ASCII
letters, digits, `-`, `_` and `.`, and a name from a hand-written file is
drawn with its markers.

## Arguments of other programs

The same outside text goes into command lines. nfu never runs a shell, so a
value is always one argument; what is left to rule out is a value being read
as an option, or as something other than the one thing it names. Each row
says what holds that, and it is never the display cleaning.

| Command | Outside values | Can one be read as an option? |
| --- | --- | --- |
| `nix flake update -- <name>…` | Input names: from the command line, the picker's selection, a project file | Without the guards, yes: `nix flake update --refresh` is an option, and updates every input. Two guards, each enough alone. `flake.CheckInputNames` refuses any name that is not an input name by nix's rule, before nix is run, in `validateNames`, `Upstream`, `project.BuildPlan` and the dry run; the picker and the form do not offer such a name. And the names stand after `--`, which ends nix's options (read in the argument parser of nix 2.19.0, the first version that takes names here; run against 2.34.9 and 2.35.6), so the first guard is not what the second depends on. |
| `nix flake update --output-lock-file <file> -- <name>` | The input name; the file is nfu's own temporary path | The same two guards. The file comes before `--` because nothing after it is an option. |
| `nix flake metadata --json --no-write-lock-file` | None | No. |
| `nh os switch <root>` | The flake path, from the configuration, `--flake` or the environment | No. `config.Resolve` makes it absolute, so it starts with `/`. |
| `deploy <deployArgs…> .#<host>` | The host name; the host's `deployArgs` | The host, no: it is always the last argument and always starts with `.#`. The `deployArgs` are options on purpose. They are whatever the host catalog or a project file says, including options that make deploy run other commands, such as `--ssh-opts`. A project file is as trusted as a script; the plan shows every argument before a run is confirmed. |
| `git -C <dir> status --porcelain -- <paths>`, `add -- <paths>`, `commit -m <subject> -- <paths>` | The directory; the commit subject, built from input names and pins; paths of `flake.lock` and of project files | No: the directory is the value of `-C`, the subject the value of `-m`, and paths stand after `--`. A path could be read as a pattern, though: `*.toml` matches every such file, and `:(top)x` is pathspec magic even after `--`. Every git command runs with `GIT_LITERAL_PATHSPECS=1`, so a path is the name of one file. |
| `$EDITOR <path>` | The project's file name; `$EDITOR` itself is the user's own | No. `projectFilePath` makes the path absolute. Editors do not agree on `--`. |
| `sudo -v` | None | No. |

## Where the raw value goes, on purpose
## Where the raw value goes, on purpose

| Sink | Gets | Why |
| --- | --- | --- |
| `nix flake update -- <name>…` | The input names as given, after they passed nix's rule for a name and were found in the lock | A name has to match the lock. A name with a hidden character in it fails the rule and is refused. |
| `deploy <args> .#<host>`, `nh os switch <root>` | Host names, deploy arguments and the flake path as stored | They have to name the same node and directory. |
| `git commit -m <subject> -- <paths>` | The commit subject built from raw names and pins (`lock.Message`, `ui.ShortPin`); raw paths | The history should say what was in the lock. The subject is cleaned when it is shown. |
| Project files, `projects.toml`, `runs.toml`, the `current` marker | Raw names, inputs, hosts, arguments, the run summary | They are data. `nfu project add --inputs` and `--hosts` write what they were given, after the warning. |
| The text fields of the form | Typed and pasted text with control sequences, bidirectional controls and invisible characters removed | The user is warned, and the cleaned text is what they then see in the field and what is saved. |
| Lookups and comparisons: the selection, the filter match, catalog and run-history keys, pin equality | Raw strings | Two different names must stay different. |
| `--json`, `--print` | Raw values | Data for another program or a file. |
| `$EDITOR <path>` | The raw path of the project file | The file has to be found. |
| A program reading plain output through a pipe | The cleaned text | It is the same output a terminal gets. |

## Tests that hold this in place

- The termsafe module's own tests, which run in its repository.
- `internal/tui/hostile_test.go`: the module's hostile samples (28 in v0.2.0) through
  each of the 33 ways outside text enters the three TUIs, checked in the
  view's string (`termsafe.ViewProblem`) and in the bytes a real bubbletea
  program writes (`termsafe.TerminalProblem`).
- `hostile_test.go`: the same samples through every way outside text reaches
  the plain output.
- `internal/ui/text_test.go`: the conversions, the notices and the two name
  rules, with exact wording.
- `internal/proc/lines_test.go`: what a line of nh or deploy output becomes,
  and the samples printed by a real child process.
- `internal/flake/names_test.go`: nix's rule for an input name; a lock that
  nix wrote for inputs named `--refresh`, `-n` and `we ird`
  (`testdata/flag-named`); and, against a stand-in for nix, that asking for
  such an input runs no `nix flake update` at all while an ordinary name goes
  after `--`.
- `internal/git/git_test.go`, with real git: a file called `*.toml` or
  `:(top)a.toml` is committed alone, and a subject that looks like an option
  is a subject.
- `internal/tui/argv_test.go`, `internal/project`, `internal/config`: the
  picker, the form and a plan refuse such an input; the editor's path and the
  flake root are absolute; a deploy host is always `.#<host>`.
