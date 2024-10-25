# tim

A developer's control panel for the terminal: project registry, ssh that routes
around a VPN when needed, host routes, tunnels through your own server and
database access from a project's `.env`. One Go binary plus a tiny zsh shim.

## Getting started

```sh
brew install --cask TimurTurdyev/tap/tim   # or: go install github.com/TimurTurdyev/tim@latest
tim init                              # wire tim into your shell (zsh or bash, detected)
tim config init                       # projects root, wrapper folder, pick clones for the registry
tim go                                # fuzzy picker over the registry, cd into the project
```

`config init` looks for git clones under the projects root (default: the first existing of `~/Dev`,
`~/Projects`, `~/code`, `~/src`) and asks which to add. If every project keeps its clone in a fixed
subfolder (for example `shop/app`), set that name as the wrapper: projects are then named after the
parent folder and `tim go shop .` jumps to `shop/`. Without a terminal use flags:
`tim config init --root ~/code --all -y`. Later, `tim proj scan` finds new clones,
`tim proj add [path]` and `tim proj rm <name>` edit the registry.

`tim init` appends the line to your shell's config and tells you what is left to do; `tim init zsh`
(or `tim init bash`) just prints the code for `eval`. It adds a `tim` function, since a process cannot change the
directory or `PATH` of its parent shell (`tim go`, `tim php use`), completion, a one-line note when you enter a
project folder, your aliases and, if you want it, the summary on every new terminal:

```toml
[shell]
motd = true          # tim motd in every new interactive shell
cd_info = true       # project, branch and changes when entering its folder (default on)

[shell.aliases]
gs = "git status -sb"
``` Config lives in `~/.config/tim/config.toml` (`TIM_CONFIG` overrides).

`tim` alone shows the most used commands, `tim --help` all of them grouped, `tim <command> --help` the details
with examples, `man tim` and `man tim-<command>` the same as man pages (installed next to the binary by
`make install` and by the cask). Help and errors are in Russian; colors only in a terminal, `NO_COLOR` turns them off.

## Commands

- `tim ssh [alias|project|user@host] [args]`: without a target opens a picker over aliases; checks the route to the address first.
  If it goes into a VPN tunnel or to the gateway of a previous network, offers to add
  a host route through the physical gateway, then connects. Private addresses are
  left to the VPN.
- `tim tunnel`: background ssh tunnels with a control socket; the server's route is checked against the VPN first.
  `tunnel all [server] [-x subnet]` sends all IPv4 and DNS through ssh with sshuttle, no proxy settings needed
  (the server itself and private ranges stay outside; `tunnel all off` stops it).
  `tunnel up [server]` is SOCKS5 on `tunnel.port` (1080), listening on 127.0.0.1 only, so the network sees plain ssh; `tunnel system on|off` points macOS proxy settings at it,
  `tunnel check` compares the external IP with and without it. `tunnel port <L:host:R|L:R|R> [server]`,
  `tunnel db [project] [--via server]` keeps a database tunnel for a GUI client, `tunnel down [name|all]`.
  `tunnel expose <port>` publishes a local port with cloudflared, or `--frp host:port`, or `--ssh server`.
- `tim route add <address|domain|subnet...> [--label x]`: keep a target outside the VPN. It is saved to the
  config and routed through the physical interface's gateway right away; a domain expands to all its A records,
  a subnet goes in as `-net`. Private ranges need `--force`, since they usually live behind the VPN.
- `tim route` shows where the internet traffic goes and the path of every target now; `route rm`, `route up|down`,
  `route clean [--all]` (drop routes to the gateway of a previous network), `route check`, `route list`, `route table`.
- `tim vpn`: tunnels, connected VPN services, full or split tunnel, DNS, how many targets bypass it;
  `tim vpn exclude <target>` is `route add`, `tim vpn fix` cleans stale routes and brings every target up again.
  IPv4 only.
- `tim db [project] [dump|restore <file>] [--via <alias>]`: mysql, psql or sqlite
  from the project's `.env`; `--via` opens an ssh port forward to the server's database
  on a free local port and closes it when the client exits.
- `tim proj list|info|here`, `tim go [project] [.]`: without a project opens a fuzzy picker.
- `tim new <name> [--stack laravel|php|go|node|vite|python|empty] [--in folder] [--no-git] [--open]`: create a
  project in the registry layout (`<root>/<in>/<name>`, plus the wrapper folder if set), initialize the stack,
  `git init` and add it to the registry with a free port for PHP. Own templates in the config:

  ```toml
  [[template]]
  name = "static"
  steps = ["mkdir -p {dir}/public", "cp ~/templates/index.html {dir}/public/"]
  port = false
  ```
- `tim config`, `tim config init`, `tim proj scan [--add]`, `tim proj add [path] [--name]`, `tim proj rm <name>`.
- `tim net check`: interface and gateway, internet, DNS, external IP, VPN, step by step with the first failure named.
- `tim ports [--all]`: who listens on TCP ports, for how long and in which project; `tim port <n> [kill] [--force]`;
  `tim stale [--hours 8] [--kill]`: project dev servers left running.
- `tim dns flush`, `tim dns servers`, `tim ssl [domain...]`: certificate expiry for the given domains or the
  `url` field of projects (`tim proj add --url`).
- `tim domain <domain|project> [--fast] [--map] [--json] [--all]`: the whole picture for a domain in one command.
  Registration and NS, mail records, subdomains from certificate transparency logs plus a probe of ~70 common names,
  every address with its network, country and PTR, what each name answers over HTTP, names in the same certificates,
  analytics counters and neighbours on the apex IP (skipped for CDN addresses). Certificate-log answers are cached for a day (`--fresh` refetches; a stale list is used when the service is down).
  `--map` writes a self-contained HTML
  graph (no external scripts) and opens it. The domain is sent to crt.sh, whois.cymru.com and api.hackertarget.com.
- `tim mail <domain> [--selector s]`: MX, SPF, DKIM (common selectors and yours), DMARC, with what is wrong in them.
- `tim http <url> [-L] [--all-headers]`: one request on a fresh connection split into DNS, TCP, TLS, first byte and
  download, status, protocol, size and key headers; `-L` follows redirects.
- `tim cert <host[:port]>`, `tim dns <host> [--server ip]`, `tim port check <host:port...>`.
- `tim site <domain|url> [--json]`: IPs with country and provider, domain registration (RDAP, whois
  for zones without RDAP), certificate, HTTP status and redirects, server and a guess at the CMS.
  Geolocation is asked from ipwho.is.

Health check:

- `tim doctor`: config and projects root, registry (missing folders, duplicate names, shared ports, unknown ssh
  aliases, broken route targets and tasks), the shell shim, `PATH` (duplicates, missing folders, programs shadowed
  by another copy), version managers that fight over one language, several brew formulas for one database client,
  crashed brew services. Read-only; exits 1 only on errors.

Disk:

- `tim disk [folder] [--min 50M]`: space taken by node_modules, vendor, venvs, target, dist, build, .next and .nuxt
  across projects, largest first with the owning project, plus totals by kind. Read-only.

Summary:

- `tim motd [section...] [--all] [--time]`: system (host, macOS, uptime, load, memory, disk, battery, local IP,
  Docker containers), project in the current folder (branch, changes), network and VPN,
  running project servers, brew services from launchd, and on request `repos` (uncommitted or unpushed),
  `procs` (top CPU), `versions`. Sections run in parallel with a timeout each; the default set
  (`system project net ports services`, or the `motd` list in the config) takes about 150 ms.
- `tim ip`: external IP with country, city and provider, directly and through the SOCKS tunnel if it is up.
- `tim wifi`: network, channel and band, standard, rate, signal and noise with a rating, security.

Across projects:

- `tim each [task] [project...] [--list] [--all] [--scan]`: run a command in every project folder (and the wrapper
  above the clone) that has a marker file. Tasks live in the config:

  ```toml
  [[task]]
  name = "deps"
  marker = "composer.json"
  run = "composer outdated --direct"
  # optional: a version key in a JSON marker and a command for the latest version;
  # then only outdated folders run unless --all
  version_key = "version"
  version_cmd = "tool --version"
  ```

  One-off: `tim each --marker go.mod -- go mod tidy`. `--dry-run` prints the commands.

Local run:

- `tim serve [project|part of a name|@group...] [-d] [--port N]`: dev server by stack (Laravel `composer dev` or
  `artisan serve`, plain PHP `php -S`, node and VitePress via `make dev` or the lockfile's package manager,
  `go run`, Python via uv or the venv, `docker compose up` when a compose file sits in the folder or in `docker/`).
  A project's `serve = "..."` field overrides the guess. PHP projects get a php from brew that fits
  `require.php` when the active one does not. Port: argument, the project's `port`, `APP_URL`, else 8000.
  Several projects, a group from `[groups]` or `-d` run in the background; busy ports move to the next free one.
  `tim serve list`, `tim serve logs <name> [-f]`, `tim serve stop [name|all]` (stops child processes too).
- `tim env [project] [--all]`: `.env` summary with secrets masked, checked against the registry.
- `tim logs [project] [-f] [-n 50]`: tail of the newest `*.log` of the project.
- `tim open [project]`: the `editor` command from the config, `$VISUAL`, or Finder.
- `tim svc [start|stop|restart|logs <name>]`: Homebrew services.
- `tim php`: installed versions, the active one and what the project's composer.json requires;
  `tim php exec 8.3 composer install` runs once with that version, `tim php use 8.3` switches the current shell
  (through the `tim init zsh` shim).

Utilities, all on the standard library; without an argument they read stdin:

- `tim hex 255`, `tim dec ff 0x1F 0b101`: number bases.
- `tim b64 [-d] [--url] <text>`, `tim jwt <token>` (no signature check), `tim hash [md5|sha1|sha256|sha512|crc32]`.
- `tim ts [epoch|date] [--tz zone]`, `tim uuid [-7] [-n N]`, `tim json [-c]`.
- `tim url encode|decode|parse`, `tim pass [-n 20] [-c N] [--no-symbols] [--no-ambiguous]`.
- `tim qr <text> [--invert]`: QR code right in the terminal, to move a link or a Wi-Fi password to a phone.
- `tim cron "<expr>" [-n 5]`: what a cron expression means and its next runs in local time.

`--dry-run` or `TIM_DRY_RUN=1` prints every command that would change the system.

Updating: `brew upgrade --cask tim` for the cask, `tim self-update [--check]` for any other install
(latest GitHub release, sha256 checked against `checksums.txt`, rollback if the swap fails).
The binary is not notarized, so the cask clears the quarantine flag after install.

## Development

```sh
make build      # ./tim with the version from git describe
make install    # go install with the same ldflags, man pages into $(go env GOPATH)/share/man/man1
make man        # man pages into ./man
make test vet lint
make bench      # startup time on an empty config (hyperfine if installed)
make inittrace  # most expensive package init functions
make benchinit  # init cost of the cmd package via mvdan.cc/benchinit
make snapshot   # local release build into dist/ (goreleaser, nothing published)
```

`tim version` prints the version set with `-ldflags -X github.com/TimurTurdyev/tim/cmd.version=...`,
falling back to the module version from `go install` or `dev`.

Interactive parts (project and ssh pickers, confirmations) live in `internal/tui`, the only
package that imports charm. They draw on `/dev/tty`, so `tim go` works inside the shim's
command substitution; without a terminal the commands fall back to plain lists and line prompts.
Startup budget: under 10 ms, since the shim runs in every new terminal.

## Migrating from the zsh tools module

If you used the zsh `tools` module, the hidden `tim config migrate <dir>` converts `tools.projects`,
`tools.hosts` and `tools.conf` from that folder into the config.

## Releasing

Push a `v*` tag: the release workflow runs goreleaser, which builds a universal macOS binary, publishes the
GitHub release with `checksums.txt` and updates the cask in `TimurTurdyev/homebrew-tap`
(needs the `HOMEBREW_TAP_TOKEN` secret with write access to that repository).
