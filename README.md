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

- `tim ssh <alias|project|user@host> [args]`: checks the route to the address first.
  If it goes into a VPN tunnel or to the gateway of a previous network, offers to add
  a host route through the physical gateway, then connects. Private addresses are
  left to the VPN.
- `tim route`, `tim route check`, `tim route up|down [addr...]`, `tim route table`.
- `tim db [project] [dump|restore <file>] [--via <alias>]`: mysql, psql or sqlite
  from the project's `.env`; `--via` opens an ssh port forward to the server's database
  on a free local port and closes it when the client exits.
- `tim proj list|info|here`, `tim go <project> [.]`.
- `tim config`, `tim config migrate <dir>`.

`--dry-run` or `TIM_DRY_RUN=1` prints every command that would change the system.
