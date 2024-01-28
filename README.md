# tim

A developer's control panel for the terminal: project registry, ssh that routes
around a VPN when needed, host routes, tunnels through your own server and
database access from a project's `.env`. One Go binary plus a tiny zsh shim.

## Install

```sh
go install github.com/TimurTurdyev/tim@latest
echo 'eval "$(tim init zsh)"' >> ~/.zshrc
```

The shim is needed for `tim go`, since a process cannot change the directory of
its parent shell. Completion comes with it.

Config lives in `~/.config/tim/config.toml`. If you used the zsh version from
iterm-config, `tim config migrate ~/Dev/projects/iterm/shell` converts
`tools.projects`, `tools.hosts` and `tools.conf` into it.

## Commands

- `tim ssh [alias|project|user@host] [args]`: without a target opens a picker over aliases; checks the route to the address first.
  If it goes into a VPN tunnel or to the gateway of a previous network, offers to add
  a host route through the physical gateway, then connects. Private addresses are
  left to the VPN.
- `tim route`, `tim route check`, `tim route up|down [addr...]`, `tim route table`.
- `tim db [project] [dump|restore <file>] [--via <alias>]`: mysql, psql or sqlite
  from the project's `.env`; `--via` opens an ssh port forward to the server's database
  on a free local port and closes it when the client exits.
- `tim proj list|info|here`, `tim go [project] [.]`: without a project opens a fuzzy picker.
- `tim config`, `tim config migrate <dir>`.
- `tim cert <host[:port]>`, `tim dns <host> [--server ip]`, `tim port check <host:port...>`.
- `tim site <domain|url> [--json]`: IPs with country and provider, domain registration (RDAP, whois
  for zones without RDAP), certificate, HTTP status and redirects, server and a guess at the CMS.
  Geolocation is asked from ipwho.is.

Utilities, all on the standard library; without an argument they read stdin:

- `tim hex 255`, `tim dec ff 0x1F 0b101`: number bases.
- `tim b64 [-d] [--url] <text>`, `tim jwt <token>` (no signature check), `tim hash [md5|sha1|sha256|sha512|crc32]`.
- `tim ts [epoch|date] [--tz zone]`, `tim uuid [-7] [-n N]`, `tim json [-c]`.

`--dry-run` or `TIM_DRY_RUN=1` prints every command that would change the system.

## Development

```sh
make build      # ./tim with the version from git describe
make install    # go install with the same ldflags
make test vet lint
make bench      # startup time on an empty config (hyperfine if installed)
make inittrace  # most expensive package init functions
make benchinit  # init cost of the cmd package via mvdan.cc/benchinit
```

`tim version` prints the version set with `-ldflags -X github.com/TimurTurdyev/tim/cmd.version=...`,
falling back to the module version from `go install` or `dev`.

Interactive parts (project and ssh pickers, confirmations) live in `internal/tui`, the only
package that imports charm. They draw on `/dev/tty`, so `tim go` works inside the shim's
command substitution; without a terminal the commands fall back to plain lists and line prompts.
Startup budget: under 10 ms, since the shim runs in every new terminal.
