# mac-schedule

A small macOS CLI that makes `launchd` user jobs easy to create, list, run, remove, and inspect.

It writes:

- metadata: `~/.local/share/mac-schedule/jobs.json`
- logs: `~/.local/state/mac-schedule/logs/`
- plists: `~/Library/LaunchAgents/dev.mac-schedule.<name>.plist`

## Install

Install the latest build from `main`:

```bash
curl -fsSL https://raw.githubusercontent.com/avyayv/mac-schedule/main/install.sh | sh
```

By default this installs `schedule` to `~/.local/bin`. Make sure that directory is on your `PATH`.

Options:

```bash
# install somewhere else
curl -fsSL https://raw.githubusercontent.com/avyayv/mac-schedule/main/install.sh | sh -s -- --dir /usr/local/bin

# install a specific release tag
curl -fsSL https://raw.githubusercontent.com/avyayv/mac-schedule/main/install.sh | sh -s -- --version latest

# build from a local checkout instead of downloading a release
./install.sh --from-source
```

Every push to `main` publishes fresh macOS arm64 and x86_64 artifacts to the moving `latest` release. Re-run the install command to update.

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
schedule version
```

## Examples

Run every 3 hours from 9am through 9pm local time:

```bash
schedule add digest --every 3h --between 09:00-21:00 -- ~/bin/digest.sh
```

Run every day at 2:30am:

```bash
schedule add backup --at 02:30 -- ~/bin/backup
```

Run every 15 minutes:

```bash
schedule add heartbeat --every 15m -- curl -fsS https://example.com/ping
```

Use five-field cron syntax:

```bash
schedule add weekdays --cron "0 9 * * MON-FRI" -- ~/bin/weekday-task
schedule add hourly --cron "@hourly" -- ~/bin/hourly-task
```

Cron fields support `*`, lists, ranges, steps, month/weekday names, and common macros. Expressions that restrict both day-of-month and weekday are rejected because launchd cannot represent cron's OR semantics without duplicate runs.

List jobs:

```bash
schedule list
```

View launchd status and paths:

```bash
schedule show digest
```

Start a job now:

```bash
schedule run digest
```

Tail logs:

```bash
schedule logs digest
schedule logs digest --err -n 200
```
