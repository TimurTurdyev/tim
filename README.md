# tim

A developer's control panel for the terminal: project registry, ssh that routes
around a VPN when needed, host routes, tunnels through your own server and
database access from a project's `.env`. One Go binary plus a tiny zsh shim.

## Getting started

```sh
go install github.com/TimurTurdyev/tim@latest
tim config init                       # projects root, wrapper folder, pick clones for the registry
echo 'eval "$(tim init zsh)"' >> ~/.zshrc
tim go                                # fuzzy picker over the registry, cd into the project
```

`config init` looks for git clones under the projects root (default: the first existing of `~/Dev`,
`~/Projects`, `~/code`, `~/src`) and asks which to add. If every project keeps its clone in a fixed
subfolder (for example `shop/app`), set that name as the wrapper: projects are then named after the
parent folder and `tim go shop .` jumps to `shop/`. Without a terminal use flags:
`tim config init --root ~/code --all -y`. Later, `tim proj scan` finds new clones,
`tim proj add [path]` and `tim proj rm <name>` edit the registry.

The shim is needed for `tim go`, since a process cannot change the directory of its parent shell.
Completion comes with it. Config lives in `~/.config/tim/config.toml` (`TIM_CONFIG` overrides).

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
- `tim config`, `tim config init`, `tim proj scan [--add]`, `tim proj add [path] [--name]`, `tim proj rm <name>`.
- `tim net check`: interface and gateway, internet, DNS, external IP, VPN, step by step with the first failure named.
- `tim ports [--all]`: who listens on TCP ports, for how long and in which project; `tim port <n> [kill] [--force]`;
  `tim stale [--hours 8] [--kill]`: project dev servers left running.
- `tim dns flush`, `tim dns servers`, `tim ssl [domain...]`: certificate expiry for the given domains or the
  `url` field of projects (`tim proj add --url`).
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

## Migrating from the zsh tools module

If you used the zsh `tools` module, the hidden `tim config migrate <dir>` converts `tools.projects`,
`tools.hosts` and `tools.conf` from that folder into the config.
