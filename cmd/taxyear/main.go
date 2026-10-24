// Command taxyear computes a tax year from a TaxYearScenarios file, JSON
// with comments, and prints the TaxYearResult as JSON.
//
//	taxyear FILE.jsonc            the whole TaxYearResult, as JSON
//	taxyear --summary FILE.jsonc  a short text summary of each scenario
//	taxyear --path P FILE.jsonc   one figure of each scenario, by field path
//
// It computes as of the file's as_of. It exits 1 when the file or a
// scenario has problems, which it prints, and 2 on bad flags or a file it
// can't read. See docs/taxyear-cli.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/nexteggapp/taxcalcs/taxyear"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main with its streams, so a test can call it. It returns the exit
// code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("taxyear", flag.ContinueOnError)
	flags.SetOutput(stderr)
	summary := flags.Bool("summary", false, "print a short text summary instead of JSON")
	path := flags.String("path", "", "print one figure of each scenario, by field path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: taxyear [--summary | --path P] FILE.jsonc")
		return 2
	}
	text, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	file, err := taxyear.Parse(text)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := taxyear.Compute(file)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	switch {
	case *path != "":
		if err := printPath(stdout, result, *path); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	case *summary:
		printSummary(stdout, result)
	default:
		b, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(result.Proto())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintln(stdout, string(b))
	}
	if hasProblems(result) {
		return 1
	}
	return 0
}

func hasProblems(r taxyear.Result) bool {
	if len(r.Problems()) > 0 {
		return true
	}
	for _, s := range r.Scenarios() {
		if len(s.Proto().GetProblems()) > 0 {
			return true
		}
	}
	return false
}

func printPath(w io.Writer, r taxyear.Result, p string) error {
	path, err := taxyear.ParsePath(p)
	if err != nil {
		return err
	}
	found := false
	for _, s := range r.Scenarios() {
		f, ok := s.Figure(path)
		if !ok {
			continue
		}
		found = true
		if a, ok := f.Amount(); ok {
			fmt.Fprintf(w, "%s\t%s\t%v\n", s.ID(), path, a)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%v\n", s.ID(), path, f.Value())
	}
	if !found {
		return errors.New("taxyear: no scenario has a figure at " + p)
	}
	return nil
}
