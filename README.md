# mac-schedule

A small Go CLI that makes macOS `launchd` user jobs easy to create, list, run, remove, and inspect.

It writes:

- metadata: `~/.local/share/mac-schedule/jobs.json`
- logs: `~/.local/state/mac-schedule/logs/`
- plists: `~/Library/LaunchAgents/com.avyay.mac-schedule.<name>.plist`

## Install

```bash
go install .
# or
./install.sh
```

The install script builds `schedule` into `~/.local/bin/schedule`.

## Usage

```bash
schedule add NAME [flags] -- COMMAND...
schedule list
schedule show NAME
schedule run NAME
schedule logs NAME [--err] [-n 100]
schedule remove NAME
schedule enable NAME
schedule disable NAME
```

## Examples

Run every 3 hours from 9am through 9pm local time:

```bash
schedule add twitter --every 3h --between 09:00-21:00 -- /Users/avyay/.pi/agent-personal/twitter-digest/hourly-twitter-imessage.sh
```

Run every day at 2:30am:

```bash
schedule add backup --at 02:30 -- ~/bin/backup
```

Run every 15 minutes:

```bash
schedule add heartbeat --every 15m -- curl -fsS https://example.com/ping
```

List jobs:

```bash
schedule list
```

View launchd status and paths:

```bash
schedule show twitter
```

Start a job now:

```bash
schedule run twitter
```

Tail logs:

```bash
schedule logs twitter
schedule logs twitter --err -n 200
```
