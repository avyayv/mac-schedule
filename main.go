package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

const (
	labelPrefix = "com.avyay.mac-schedule"
	appName     = "mac-schedule"
)

type Job struct {
	Name        string            `json:"name"`
	Label       string            `json:"label"`
	Command     string            `json:"command"`
	WorkingDir  string            `json:"working_dir"`
	Schedule    Schedule          `json:"schedule"`
	RunAtLoad   bool              `json:"run_at_load"`
	PlistPath   string            `json:"plist_path"`
	StdoutPath  string            `json:"stdout_path"`
	StderrPath  string            `json:"stderr_path"`
	Environment map[string]string `json:"environment,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type Schedule struct {
	Kind          string      `json:"kind"` // interval or calendar
	EverySeconds  int         `json:"every_seconds,omitempty"`
	Times         []TimeOfDay `json:"times,omitempty"`
	Between       string      `json:"between,omitempty"`
	EveryOriginal string      `json:"every_original,omitempty"`
}

type TimeOfDay struct {
	Hour   int `json:"hour"`
	Minute int `json:"minute"`
}

type Store struct {
	Jobs []Job `json:"jobs"`
}

type launchStatus struct {
	Loaded   bool
	Running  bool
	PID      string
	LastCode string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(exitCode(err))
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stdout)
		return nil
	}

	switch args[0] {
	case "add":
		return cmdAdd(args[1:], stdout)
	case "list", "ls":
		return cmdList(args[1:], stdout)
	case "show":
		return cmdShow(args[1:], stdout)
	case "remove", "rm":
		return cmdRemove(args[1:], stdout)
	case "enable":
		return cmdEnable(args[1:], stdout)
	case "disable":
		return cmdDisable(args[1:], stdout)
	case "run":
		return cmdRunNow(args[1:], stdout)
	case "logs":
		return cmdLogs(args[1:], stdout)
	case "path":
		return cmdPath(args[1:], stdout)
	case "help", "--help", "-h":
		usage(stdout)
		return nil
	default:
		return usageError("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `schedule - small launchd wrapper for user scheduled jobs

Usage:
  schedule add NAME [flags] -- COMMAND...
  schedule list
  schedule show NAME
  schedule run NAME
  schedule logs NAME [--err] [-n lines]
  schedule remove NAME
  schedule enable NAME
  schedule disable NAME

Examples:
  schedule add twitter --every 3h --between 09:00-21:00 -- /path/to/twitter.sh
  schedule add backup --at 02:30 -- ~/bin/backup
  schedule add heartbeat --every 15m -- curl -fsS https://example.com/ping

Add flags:
  --every duration       interval such as 15m, 3h, 24h
  --between HH:MM-HH:MM  with --every, create calendar triggers in this local-time window
  --at HH:MM[,HH:MM]     exact local times each day
  --cwd path             working directory (default: current directory)
  --run-at-load          run when job is loaded
  --env KEY=VALUE        extra environment variable (repeatable)
  --replace              replace an existing job

Files:
  metadata: ~/.local/share/mac-schedule/jobs.json
  logs:     ~/.local/state/mac-schedule/logs
  plists:   ~/Library/LaunchAgents/com.avyay.mac-schedule.<name>.plist
`)
}

func cmdAdd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var every, between, at, cwd string
	var runAtLoad, replace bool
	var envs arrayFlags
	fs.StringVar(&every, "every", "", "interval duration")
	fs.StringVar(&between, "between", "", "time window HH:MM-HH:MM")
	fs.StringVar(&at, "at", "", "comma-separated HH:MM times")
	fs.StringVar(&cwd, "cwd", "", "working directory")
	fs.BoolVar(&runAtLoad, "run-at-load", false, "run at load")
	fs.BoolVar(&replace, "replace", false, "replace existing job")
	fs.Var(&envs, "env", "KEY=VALUE environment variable")
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return usageError("add requires NAME first")
	}
	name := args[0]
	sep := -1
	for i, arg := range args[1:] {
		if arg == "--" {
			sep = i + 1
			break
		}
	}
	if sep == -1 {
		return usageError("add requires -- before COMMAND")
	}
	flagArgs := args[1:sep]
	commandArgs := args[sep+1:]
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(commandArgs) == 0 {
		return usageError("command cannot be empty")
	}

	command := strings.Join(commandArgs, " ")
	if command == "" {
		return usageError("command cannot be empty")
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}

	sched, err := buildSchedule(every, between, at)
	if err != nil {
		return err
	}

	store, err := loadStore()
	if err != nil {
		return err
	}
	if existing := store.find(name); existing != nil && !replace {
		return usageError("job %q already exists; use --replace", name)
	}
	if existing := store.find(name); existing != nil && replace {
		_ = unloadJob(existing.Label, existing.PlistPath)
		_ = os.Remove(existing.PlistPath)
		store.remove(name)
	}

	now := time.Now()
	label := labelPrefix + "." + slug(name)
	paths, err := pathsFor(name)
	if err != nil {
		return err
	}
	job := Job{
		Name:        name,
		Label:       label,
		Command:     command,
		WorkingDir:  absCwd,
		Schedule:    sched,
		RunAtLoad:   runAtLoad,
		PlistPath:   paths.plist,
		StdoutPath:  paths.stdout,
		StderrPath:  paths.stderr,
		Environment: parseEnv(envs),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := ensureDirs(); err != nil {
		return err
	}
	if err := writePlist(job); err != nil {
		return err
	}
	if err := bootstrapJob(job.PlistPath); err != nil {
		return err
	}
	store.Jobs = append(store.Jobs, job)
	sortJobs(store.Jobs)
	if err := saveStore(store); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "added %s (%s)\n", job.Name, describeSchedule(job.Schedule))
	return nil
}

func cmdList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var out string
	fs.StringVar(&out, "output", "table", "table or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := loadStore()
	if err != nil {
		return err
	}
	if out == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(store.Jobs)
	}
	if out != "table" {
		return usageError("unsupported output %q", out)
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATUS\tSCHEDULE\tCOMMAND")
	for _, job := range store.Jobs {
		st := getLaunchStatus(job.Label)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", job.Name, formatStatus(st), describeSchedule(job.Schedule), job.Command)
	}
	return tw.Flush()
}

func cmdShow(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError("show requires NAME")
	}
	job, err := getJob(args[0])
	if err != nil {
		return err
	}
	st := getLaunchStatus(job.Label)
	fmt.Fprintf(stdout, "Name:       %s\n", job.Name)
	fmt.Fprintf(stdout, "Label:      %s\n", job.Label)
	fmt.Fprintf(stdout, "Status:     %s\n", formatStatus(st))
	fmt.Fprintf(stdout, "Schedule:   %s\n", describeSchedule(job.Schedule))
	fmt.Fprintf(stdout, "Command:    %s\n", job.Command)
	fmt.Fprintf(stdout, "WorkingDir: %s\n", job.WorkingDir)
	fmt.Fprintf(stdout, "Plist:      %s\n", job.PlistPath)
	fmt.Fprintf(stdout, "Stdout:     %s\n", job.StdoutPath)
	fmt.Fprintf(stdout, "Stderr:     %s\n", job.StderrPath)
	return nil
}

func cmdRemove(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError("remove requires NAME")
	}
	store, err := loadStore()
	if err != nil {
		return err
	}
	job := store.find(args[0])
	if job == nil {
		return usageError("job %q not found", args[0])
	}
	_ = unloadJob(job.Label, job.PlistPath)
	_ = os.Remove(job.PlistPath)
	store.remove(job.Name)
	if err := saveStore(store); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "removed %s\n", job.Name)
	return nil
}

func cmdEnable(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError("enable requires NAME")
	}
	job, err := getJob(args[0])
	if err != nil {
		return err
	}
	if err := bootstrapJob(job.PlistPath); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "enabled %s\n", job.Name)
	return nil
}

func cmdDisable(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError("disable requires NAME")
	}
	job, err := getJob(args[0])
	if err != nil {
		return err
	}
	if err := unloadJob(job.Label, job.PlistPath); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "disabled %s\n", job.Name)
	return nil
}

func cmdRunNow(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return usageError("run requires NAME")
	}
	job, err := getJob(args[0])
	if err != nil {
		return err
	}
	cmd := exec.Command("launchctl", "kickstart", "-k", domain()+"/"+job.Label)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(stdout, "started %s\n", job.Name)
	return nil
}

func cmdLogs(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var n int
	var errLog bool
	fs.IntVar(&n, "n", 80, "lines")
	fs.BoolVar(&errLog, "err", false, "show stderr log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return usageError("logs requires NAME")
	}
	job, err := getJob(fs.Args()[0])
	if err != nil {
		return err
	}
	path := job.StdoutPath
	if errLog {
		path = job.StderrPath
	}
	cmd := exec.Command("tail", "-n", strconv.Itoa(n), path)
	cmd.Stdout = stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func cmdPath(args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError("path takes no args")
	}
	root, err := dataDir()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, root)
	return nil
}

func buildSchedule(every, between, at string) (Schedule, error) {
	if at != "" && every != "" {
		return Schedule{}, usageError("use either --at or --every, not both")
	}
	if at == "" && every == "" {
		return Schedule{}, usageError("schedule requires --at or --every")
	}
	if at != "" {
		times, err := parseTimes(at)
		if err != nil {
			return Schedule{}, err
		}
		return Schedule{Kind: "calendar", Times: times}, nil
	}
	d, err := time.ParseDuration(every)
	if err != nil || d <= 0 {
		return Schedule{}, usageError("invalid --every duration %q", every)
	}
	if between == "" {
		return Schedule{Kind: "interval", EverySeconds: int(d.Seconds()), EveryOriginal: every}, nil
	}
	start, end, err := parseWindow(between)
	if err != nil {
		return Schedule{}, err
	}
	if d < time.Minute || int(d.Minutes()) == 0 {
		return Schedule{}, usageError("--every with --between must be at least 1m")
	}
	var times []TimeOfDay
	stepMinutes := int(d.Minutes())
	startMinutes := start.Hour*60 + start.Minute
	endMinutes := end.Hour*60 + end.Minute
	for total := startMinutes; total <= endMinutes; total += stepMinutes {
		times = append(times, TimeOfDay{Hour: total / 60, Minute: total % 60})
		if len(times) > 1440 {
			return Schedule{}, usageError("too many calendar triggers")
		}
	}
	return Schedule{Kind: "calendar", Times: times, Between: between, EveryOriginal: every}, nil
}

func parseTimes(s string) ([]TimeOfDay, error) {
	parts := strings.Split(s, ",")
	seen := map[TimeOfDay]bool{}
	var times []TimeOfDay
	for _, part := range parts {
		t, err := parseHHMM(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		if !seen[t] {
			seen[t] = true
			times = append(times, t)
		}
	}
	sort.Slice(times, func(i, j int) bool {
		if times[i].Hour == times[j].Hour {
			return times[i].Minute < times[j].Minute
		}
		return times[i].Hour < times[j].Hour
	})
	return times, nil
}

func parseWindow(s string) (TimeOfDay, TimeOfDay, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return TimeOfDay{}, TimeOfDay{}, usageError("invalid --between %q; expected HH:MM-HH:MM", s)
	}
	start, err := parseHHMM(strings.TrimSpace(parts[0]))
	if err != nil {
		return TimeOfDay{}, TimeOfDay{}, err
	}
	end, err := parseHHMM(strings.TrimSpace(parts[1]))
	if err != nil {
		return TimeOfDay{}, TimeOfDay{}, err
	}
	if afterTime(start, end) {
		return TimeOfDay{}, TimeOfDay{}, usageError("--between cannot cross midnight yet")
	}
	return start, end, nil
}

func parseHHMM(s string) (TimeOfDay, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return TimeOfDay{}, usageError("invalid time %q; expected HH:MM", s)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return TimeOfDay{}, usageError("invalid hour in %q", s)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return TimeOfDay{}, usageError("invalid minute in %q", s)
	}
	return TimeOfDay{Hour: hour, Minute: minute}, nil
}

func afterTime(a, b TimeOfDay) bool {
	if a.Hour == b.Hour {
		return a.Minute > b.Minute
	}
	return a.Hour > b.Hour
}

func describeSchedule(s Schedule) string {
	switch s.Kind {
	case "interval":
		return "every " + durationText(s.EverySeconds)
	case "calendar":
		parts := make([]string, 0, len(s.Times))
		for _, t := range s.Times {
			parts = append(parts, fmt.Sprintf("%02d:%02d", t.Hour, t.Minute))
		}
		if s.EveryOriginal != "" && s.Between != "" {
			return fmt.Sprintf("every %s between %s (%s)", s.EveryOriginal, s.Between, strings.Join(parts, ","))
		}
		return "at " + strings.Join(parts, ",")
	default:
		return s.Kind
	}
}

func durationText(seconds int) string {
	d := time.Duration(seconds) * time.Second
	if d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return d.String()
}

func writePlist(job Job) error {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	writeKeyString(&buf, "Label", job.Label)
	buf.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	writeString(&buf, "/bin/bash")
	writeString(&buf, "-lc")
	writeString(&buf, job.Command)
	buf.WriteString("  </array>\n")
	writeKeyBool(&buf, "RunAtLoad", job.RunAtLoad)
	writeKeyString(&buf, "WorkingDirectory", job.WorkingDir)
	writeSchedule(&buf, job.Schedule)
	writeKeyString(&buf, "StandardOutPath", job.StdoutPath)
	writeKeyString(&buf, "StandardErrorPath", job.StderrPath)
	buf.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n")
	env := defaultEnv()
	for k, v := range job.Environment {
		env[k] = v
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeKeyString(&buf, k, env[k])
	}
	buf.WriteString("  </dict>\n")
	buf.WriteString("</dict>\n</plist>\n")
	return os.WriteFile(job.PlistPath, buf.Bytes(), 0644)
}

func writeSchedule(buf *bytes.Buffer, s Schedule) {
	if s.Kind == "interval" {
		buf.WriteString("  <key>StartInterval</key>\n")
		fmt.Fprintf(buf, "  <integer>%d</integer>\n", s.EverySeconds)
		return
	}
	buf.WriteString("  <key>StartCalendarInterval</key>\n  <array>\n")
	for _, t := range s.Times {
		buf.WriteString("    <dict>\n")
		buf.WriteString("      <key>Hour</key>\n")
		fmt.Fprintf(buf, "      <integer>%d</integer>\n", t.Hour)
		buf.WriteString("      <key>Minute</key>\n")
		fmt.Fprintf(buf, "      <integer>%d</integer>\n", t.Minute)
		buf.WriteString("    </dict>\n")
	}
	buf.WriteString("  </array>\n")
}

func writeKeyString(buf *bytes.Buffer, key, value string) {
	fmt.Fprintf(buf, "  <key>%s</key>\n", xmlEscape(key))
	writeString(buf, value)
}

func writeString(buf *bytes.Buffer, value string) {
	fmt.Fprintf(buf, "    <string>%s</string>\n", xmlEscape(value))
}

func writeKeyBool(buf *bytes.Buffer, key string, value bool) {
	fmt.Fprintf(buf, "  <key>%s</key>\n", xmlEscape(key))
	if value {
		buf.WriteString("  <true/>\n")
	} else {
		buf.WriteString("  <false/>\n")
	}
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

type appPaths struct {
	plist  string
	stdout string
	stderr string
}

func pathsFor(name string) (appPaths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return appPaths{}, err
	}
	state, err := stateDir()
	if err != nil {
		return appPaths{}, err
	}
	s := slug(name)
	return appPaths{
		plist:  filepath.Join(home, "Library", "LaunchAgents", labelPrefix+"."+s+".plist"),
		stdout: filepath.Join(state, "logs", s+".out.log"),
		stderr: filepath.Join(state, "logs", s+".err.log"),
	}, nil
}

func defaultEnv() map[string]string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/opt/homebrew/bin:/usr/local/bin:/Users/avyay/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	home, _ := os.UserHomeDir()
	return map[string]string{
		"HOME": home,
		"PATH": path,
	}
}

func ensureDirs() error {
	data, err := dataDir()
	if err != nil {
		return err
	}
	state, err := stateDir()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, dir := range []string{data, filepath.Join(state, "logs"), filepath.Join(home, "Library", "LaunchAgents")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func dataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName), nil
}

func stateDir() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", appName), nil
}

func storePath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "jobs.json"), nil
}

func loadStore() (*Store, error) {
	path, err := storePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func saveStore(s *Store) error {
	if err := ensureDirs(); err != nil {
		return err
	}
	path, err := storePath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0644)
}

func (s *Store) find(name string) *Job {
	for i := range s.Jobs {
		if s.Jobs[i].Name == name || slug(s.Jobs[i].Name) == slug(name) {
			return &s.Jobs[i]
		}
	}
	return nil
}

func (s *Store) remove(name string) {
	out := s.Jobs[:0]
	for _, job := range s.Jobs {
		if job.Name != name && slug(job.Name) != slug(name) {
			out = append(out, job)
		}
	}
	s.Jobs = out
}

func getJob(name string) (*Job, error) {
	store, err := loadStore()
	if err != nil {
		return nil, err
	}
	job := store.find(name)
	if job == nil {
		return nil, usageError("job %q not found", name)
	}
	return job, nil
}

func sortJobs(jobs []Job) {
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
}

func bootstrapJob(plistPath string) error {
	cmd := exec.Command("launchctl", "bootstrap", domain(), plistPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	text := strings.TrimSpace(string(out))
	if strings.Contains(text, "Bootstrap failed: 5") || strings.Contains(text, "Input/output error") || strings.Contains(text, "already") {
		// It may already be loaded; reload cleanly.
		_ = exec.Command("launchctl", "bootout", domain(), plistPath).Run()
		cmd = exec.Command("launchctl", "bootstrap", domain(), plistPath)
		out, err = cmd.CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func unloadJob(label, plistPath string) error {
	cmd := exec.Command("launchctl", "bootout", domain(), plistPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	cmd = exec.Command("launchctl", "bootout", domain()+"/"+label)
	out, err = cmd.CombinedOutput()
	if err != nil && !strings.Contains(strings.TrimSpace(string(out)), "No such process") {
		return fmt.Errorf("launchctl bootout: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func getLaunchStatus(label string) launchStatus {
	cmd := exec.Command("launchctl", "list")
	out, err := cmd.Output()
	if err != nil {
		return launchStatus{}
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == label {
			st := launchStatus{Loaded: true, LastCode: fields[1]}
			if fields[0] != "-" && fields[0] != "0" {
				st.Running = true
				st.PID = fields[0]
			}
			return st
		}
	}
	return launchStatus{}
}

func formatStatus(st launchStatus) string {
	if !st.Loaded {
		return "disabled"
	}
	if st.Running {
		return "running pid=" + st.PID
	}
	if st.LastCode != "-" && st.LastCode != "0" {
		return "loaded last=" + st.LastCode
	}
	return "loaded"
}

func domain() string {
	return "gui/" + strconv.Itoa(os.Getuid())
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	re := regexp.MustCompile(`[^a-z0-9_.-]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "job"
	}
	return s
}

type arrayFlags []string

func (a *arrayFlags) String() string { return strings.Join(*a, ",") }
func (a *arrayFlags) Set(v string) error {
	*a = append(*a, v)
	return nil
}

func parseEnv(values []string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	env := map[string]string{}
	for _, v := range values {
		k, val, ok := strings.Cut(v, "=")
		if ok && k != "" {
			env[k] = val
		}
	}
	return env
}

type usageErr struct{ msg string }

func (e usageErr) Error() string { return e.msg }

func usageError(format string, args ...any) error {
	return usageErr{msg: fmt.Sprintf(format, args...)}
}

func exitCode(err error) int {
	var ue usageErr
	if errors.As(err, &ue) {
		return 2
	}
	return 1
}
