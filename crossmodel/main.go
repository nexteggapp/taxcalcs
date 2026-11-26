// Command report runs generated households through the taxcalcs rules and
// through PolicyEngine-US, an independent model, and writes REPORT.md: for
// each jurisdiction and figure, how many households agree to the dollar and
// the largest differences. docs/testing.md describes the harness.
//
// The other model is a report and a source of test households, never an
// oracle. Each kind of difference is triaged in triage.tsv as our bug, their
// bug, or a difference of reading, with a citation; the command fails while
// any difference is untriaged.
//
//	bazel run //crossmodel:report             # check, and print the report
//	bazel run //crossmodel:report -- -write   # rewrite crossmodel/REPORT.md
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
)

var (
	runner = flag.String("runner", "", "rlocation of the PolicyEngine-US runner")
	seed   = flag.Int64("seed", 2025, "seed for the generated households")
	per    = flag.Int("per", 6, "households per jurisdiction")
	write  = flag.Bool("write", false, "write crossmodel/REPORT.md in the source tree")
)

func main() {
	flag.Parse()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "report:", err)
		os.Exit(1)
	}
}

func run() error {
	households := generate(rand.New(rand.NewSource(*seed)), *per)
	ours, err := prepareOurs(households)
	if err != nil {
		return err
	}
	theirs, err := runTheirs(households)
	if err != nil {
		return err
	}
	triage, err := readTriage()
	if err != nil {
		return err
	}
	report, untriaged := compare(households, ours, theirs, triage)
	if *write {
		ws := os.Getenv("BUILD_WORKSPACE_DIRECTORY")
		if ws == "" {
			return fmt.Errorf("-write needs bazel run")
		}
		if err := os.WriteFile(filepath.Join(ws, "crossmodel", "REPORT.md"), []byte(report), 0o644); err != nil {
			return err
		}
	} else {
		fmt.Print(report)
	}
	if untriaged > 0 {
		return fmt.Errorf("%d kinds of difference are not triaged; see the report", untriaged)
	}
	return nil
}

// prepareOurs prepares each household with the taxcalcs rules.
func prepareOurs(hs []Household) (map[string]map[string]float64, error) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]float64{}
	for _, h := range hs {
		ret, err := eng.Prepare(h.Inputs())
		if err != nil {
			return nil, fmt.Errorf("household %s: %w", h.ID, err)
		}
		out[h.ID] = ourFigures(ret)
	}
	return out, nil
}

// theirResult is one line of the runner's output.
type theirResult struct {
	ID      string             `json:"id"`
	Figures map[string]float64 `json:"figures"`
	Error   string             `json:"error"`
}

// runTheirs runs the households through PolicyEngine-US.
func runTheirs(hs []Household) (map[string]theirResult, error) {
	path, err := runfiles.Rlocation(*runner)
	if err != nil {
		return nil, fmt.Errorf("finding the runner %q: %w", *runner, err)
	}
	dir, err := os.MkdirTemp("", "crossmodel")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "in.jsonl"), filepath.Join(dir, "out.jsonl")
	f, err := os.Create(in)
	if err != nil {
		return nil, err
	}
	enc := json.NewEncoder(f)
	for _, h := range hs {
		if err := enc.Encode(h.theirInput()); err != nil {
			return nil, err
		}
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	cmd := exec.Command(path, in, out)
	cmd.Env, _ = runfiles.Env()
	cmd.Env = append(os.Environ(), cmd.Env...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running PolicyEngine-US: %w", err)
	}
	res, err := os.Open(out)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	results := map[string]theirResult{}
	sc := bufio.NewScanner(res)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var r theirResult
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		results[r.ID] = r
	}
	return results, sc.Err()
}

// Triage is the finding for one kind of difference: a figure in a
// jurisdiction.
type Triage struct {
	Kind, Reference, Note string
}

// readTriage reads triage.tsv: jurisdiction, figure, kind (ours, theirs or
// reading), an issue or citation, and a note.
func readTriage() (map[string]Triage, error) {
	path, err := runfiles.Rlocation("_main/crossmodel/triage.tsv")
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]Triage{}
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("triage.tsv line %d: %d fields, want 5", i+1, len(f))
		}
		switch f[2] {
		case "ours", "theirs", "reading":
		default:
			return nil, fmt.Errorf("triage.tsv line %d: kind %q is not ours, theirs or reading", i+1, f[2])
		}
		out[f[0]+"/"+f[1]] = Triage{Kind: f[2], Reference: f[3], Note: f[4]}
	}
	return out, nil
}

// compare writes the report and counts the untriaged kinds of difference.
func compare(hs []Household, ours map[string]map[string]float64, theirs map[string]theirResult, triage map[string]Triage) (string, int) {
	type cell struct {
		n, agree   int
		worst      float64
		worstID    string
		worstOurs  float64
		worstTheir float64
	}
	cells := map[string]*cell{}
	byID := map[string]Household{}
	var errors []string
	for _, h := range hs {
		byID[h.ID] = h
		t := theirs[h.ID]
		if t.Error != "" || t.Figures == nil {
			errors = append(errors, fmt.Sprintf("%s: %s", h.ID, t.Error))
			continue
		}
		for _, m := range mapping {
			jur, ourKey := "US", m.ours
			if m.state {
				if noWageTax[h.State] {
					continue
				}
				jur, ourKey = h.State, "state/"+m.ours
			}
			key := jur + "/" + m.ours
			c := cells[key]
			if c == nil {
				c = &cell{}
				cells[key] = c
			}
			o, th := ours[h.ID][ourKey], t.Figures[m.theirs]
			c.n++
			d := math.Abs(o - th)
			if d < 1 {
				c.agree++
			}
			if d > c.worst {
				c.worst, c.worstID, c.worstOurs, c.worstTheir = d, h.ID, o, th
			}
		}
	}
	var keys []string
	for k := range cells {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Cross-model report\n\n")
	fmt.Fprintf(&b, "Generated by `bazel run //crossmodel:report -- -write`: %d households, seed %d, tax year 2025, against PolicyEngine-US %s. A figure agrees when the two models are within $1. See [docs/testing.md](../docs/testing.md#cross-model-harness).\n\n", len(hs), *seed, policyEngineVersion)
	b.WriteString("| Jurisdiction | Figure | Households | Agree | Largest difference | Ours | Theirs | Household | Triage |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	untriaged := 0
	var lines []string
	for _, k := range keys {
		c := cells[k]
		jur, fig, _ := strings.Cut(k, "/")
		tr := "agrees"
		if c.agree < c.n {
			if t, ok := triage[k]; ok {
				tr = t.Kind + ": " + t.Reference
			} else {
				tr = "**untriaged**"
				untriaged++
			}
		}
		worst := ""
		if c.worstID != "" {
			worst = fmt.Sprintf("$%.2f | $%.2f | $%.2f | %s", c.worst, c.worstOurs, c.worstTheir, c.worstID)
		} else {
			worst = " | | | "
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | %d | %d | %s | %s |", jur, fig, c.n, c.agree, worst, tr))
	}
	b.WriteString(strings.Join(lines, "\n") + "\n")
	b.WriteString("\n## Households\n\n| Household | Facts |\n|---|---|\n")
	for _, h := range hs {
		fmt.Fprintf(&b, "| %s | %s |\n", h.ID, h.Describe())
	}
	if len(errors) > 0 {
		b.WriteString("\n## Households PolicyEngine-US could not compute\n\n")
		for _, e := range errors {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	return b.String(), untriaged
}
