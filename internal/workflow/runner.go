package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

type Result struct {
	ID         string   `json:"id"`
	Command    []string `json:"command,omitempty"`
	Status     string   `json:"status"`
	ExitCode   int      `json:"exit_code"`
	DurationMS int64    `json:"duration_ms"`
	Output     string   `json:"output,omitempty"`
	Truncated  bool     `json:"truncated,omitempty"`
}

type Run struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	Before        Snapshot  `json:"before"`
	After         Snapshot  `json:"after"`
	SnapshotError string    `json:"snapshot_error,omitempty"`
	Results       []Result  `json:"results"`
	Stale         bool      `json:"stale"`
	StaleReason   string    `json:"stale_reason,omitempty"`
}

type Event struct {
	Kind   string
	Index  int
	Result Result
	Run    Run
	Err    error
}

func (r Run) Success() bool {
	if r.Stale || r.SnapshotError != "" {
		return false
	}
	passed := false
	for _, result := range r.Results {
		if result.Status == "passed" {
			passed = true
		} else if result.Status != "skipped" {
			return false
		}
	}
	return passed
}

func CleanText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

func Redact(value string) string {
	value = CleanText(value)
	for _, item := range os.Environ() {
		key, secret, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(key)
		if len(secret) >= 4 && (strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "API_KEY") || strings.Contains(upper, "PRIVATE_KEY") || strings.Contains(upper, "CREDENTIAL")) {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	assignment := regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|password|secret|authorization)\s*[=:]\s*)([^\s,;]+)`)
	value = assignment.ReplaceAllString(value, "${1}[REDACTED]")
	token := regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9_]{12,}|github_pat_[A-Za-z0-9_]{12,}|sk-[A-Za-z0-9_-]{12,})`)
	return token.ReplaceAllString(value, "[REDACTED]")
}

type boundedLog struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
	updated   time.Time
	progress  func(string, bool)
}

func (b *boundedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	const limit = 64 << 10
	n := len(p)
	b.data = append(b.data, p...)
	if len(b.data) > limit {
		b.data = append([]byte(nil), b.data[len(b.data)-limit:]...)
		b.truncated = true
	}
	var update string
	send := b.progress != nil && time.Since(b.updated) >= 200*time.Millisecond
	if send {
		update = string(b.data)
		b.updated = time.Now()
	}
	truncated := b.truncated
	b.mu.Unlock()
	if send {
		b.progress(Redact(update), truncated)
	}
	return n, nil
}
func (b *boundedLog) output() (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Redact(string(b.data)), b.truncated
}

func Execute(ctx context.Context, root string, cfg Config, notify func(Event)) (Run, error) {
	if err := cfg.Validate(); err != nil {
		return Run{}, err
	}
	before, err := Fingerprint(ctx, root, cfg.Exclude)
	if err != nil {
		return Run{}, fmt.Errorf("capture source state: %w", err)
	}
	now := time.Now().UTC()
	run := Run{SchemaVersion: 1, ID: fmt.Sprintf("%s-%d", now.Format("20060102T150405"), now.Nanosecond()), StartedAt: now, Before: before, Results: []Result{}}
	emit := func(event Event) {
		if notify != nil {
			notify(event)
		}
	}
	for index, check := range cfg.Checks {
		result := Result{ID: check.ID, Command: append([]string(nil), check.Command...), Status: "running", ExitCode: -1}
		if check.SkipReason != "" {
			result.Status = "skipped"
			result.Output = check.SkipReason
		} else if ctx.Err() != nil {
			result.Status = "cancelled"
			result.Output = "Cancelled before starting."
		} else {
			emit(Event{Kind: "started", Index: index, Result: result})
			start := time.Now()
			timeout := check.TimeoutSeconds
			if timeout == 0 {
				timeout = 120
			}
			checkCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
			if check.Builtin == "docs" {
				report, checkErr := CheckDocs(checkCtx, root)
				if checkErr != nil {
					result.Status = "failed"
					result.Output = checkErr.Error()
				} else {
					result.Output = DocsText(report)
					result.ExitCode = 0
					result.Status = "passed"
					if len(report.Issues) > 0 {
						result.ExitCode = 1
						result.Status = "failed"
					}
				}
			} else {
				command, commandErr := commandFor(checkCtx, check.Command)
				if commandErr != nil {
					result.Status = "failed"
					result.Output = commandErr.Error()
				} else {
					command.Dir = root
					// CI makes common test runners use their finite, non-watch behavior.
					command.Env = append(os.Environ(), "CI=1", "NO_COLOR=1", "TERM=dumb")
					progress := result
					log := &boundedLog{progress: func(output string, truncated bool) {
						update := progress
						update.Output, update.Truncated = output, truncated
						update.DurationMS = time.Since(start).Milliseconds()
						emit(Event{Kind: "output", Index: index, Result: update})
					}}
					command.Stdout = log
					command.Stderr = log
					commandErr = command.Run()
					result.Output, result.Truncated = log.output()
					if command.ProcessState != nil {
						result.ExitCode = command.ProcessState.ExitCode()
					}
					result.Status = "passed"
					if commandErr != nil {
						result.Status = "failed"
						if result.Output == "" {
							result.Output = commandErr.Error()
						}
					}
				}
			}
			if ctx.Err() != nil {
				result.Status = "cancelled"
			} else if errors.Is(checkCtx.Err(), context.DeadlineExceeded) {
				result.Status = "timed_out"
				result.Output += "\nCheck exceeded its configured timeout."
			}
			cancel()
			result.DurationMS = time.Since(start).Milliseconds()
		}
		result.Output = Redact(result.Output)
		for i := range result.Command {
			result.Command[i] = Redact(result.Command[i])
		}
		run.Results = append(run.Results, result)
		emit(Event{Kind: "finished", Index: index, Result: result})
	}
	run.FinishedAt = time.Now().UTC()
	// Capture and persist cancellation evidence even when the execution context ended.
	snapshotCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	run.After, err = Fingerprint(snapshotCtx, root, cfg.Exclude)
	if err != nil {
		run.SnapshotError = err.Error()
		run.Stale = true
		run.StaleReason = "The final source snapshot could not be captured."
	} else if run.Before.Digest != run.After.Digest || run.Before.Commit != run.After.Commit {
		run.Stale = true
		run.StaleReason = "Source files or HEAD changed during the run."
	}
	if err := SaveRun(root, run); err != nil {
		return run, err
	}
	emit(Event{Kind: "completed", Run: run})
	return run, nil
}

func SaveRun(root string, run Run) error {
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err = WriteFile(root, StateDir+"/runs/"+run.ID+".json", data, false); err != nil {
		return err
	}
	return WriteFile(root, StateDir+"/latest.json", data, true)
}

func Latest(ctx context.Context, root string) (Run, error) {
	data, err := ReadFile(root, StateDir+"/latest.json", 4<<20)
	if err != nil {
		return Run{}, fmt.Errorf("no readable run record; run launchproof check: %w", err)
	}
	var run Run
	if err = json.Unmarshal(data, &run); err != nil || run.SchemaVersion != 1 {
		return Run{}, errors.New("invalid saved run record")
	}
	cfg, err := LoadConfig(root)
	if err != nil {
		run.Stale = true
		run.StaleReason = "Configuration is missing or invalid."
		return run, nil
	}
	current, err := Fingerprint(ctx, root, cfg.Exclude)
	if err != nil {
		run.Stale = true
		run.StaleReason = "Cannot verify the current source snapshot: " + err.Error()
	} else if current.Digest != run.Before.Digest || current.Commit != run.Before.Commit {
		run.Stale = true
		run.StaleReason = "Source files, configuration, or HEAD changed since this run."
	}
	return run, nil
}
