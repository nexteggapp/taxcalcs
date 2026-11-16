package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Estimated tax needs no registry entry of its own. A state joins from its
// files, so that states landing in parallel touch no shared line by hand:
//   - its rules are a file returns/rules/states/<st>/NN_estimated.tlisp that
//     defines <name>-estimated-answer, as alabama-estimated-answer, which the
//     dispatch in 00_states.tlisp calls for its postal code;
//   - its own facts, if its tests read any, are a message
//     <Name>EstimatedInputs in returns/v1/estimated_<st>.proto, which joins
//     the EstimatedStateInputs oneof with the state's StateInputs number,
//     and the proto_library and estimated.proto's imports.

// estimatedRule is the taxlisp rule that answers the state's estimated tax.
func estimatedRule(s State) string {
	return strings.ToLower(strings.ReplaceAll(s.Name, " ", "-")) + "-estimated-answer"
}

// estimatedInputsMessage is the state's own estimated tax facts message.
func estimatedInputsMessage(s State) string { return messageName(s) + "EstimatedInputs" }

// estimatedInputsFile is the state's own estimated tax facts file, from the
// repository root.
func estimatedInputsFile(s State) string { return "returns/v1/estimated_" + s.Postal + ".proto" }

// estimatedRulesFile matches a state's estimated tax rule file by name.
var estimatedRulesFile = regexp.MustCompile(`^[0-9]+_estimated\.tlisp$`)

// withEstimatedRules is every state with estimated tax rules, in postal-code
// order. A rule file that doesn't define the state's rule is an error.
func withEstimatedRules(root string) ([]State, error) {
	var out []State
	var errs []error
	for _, s := range byPostal() {
		dir := filepath.Join(root, "returns/rules/states", s.Postal)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var files []string
		for _, e := range entries {
			if !e.IsDir() && estimatedRulesFile.MatchString(e.Name()) {
				files = append(files, e.Name())
			}
		}
		switch len(files) {
		case 0:
			continue
		case 1:
		default:
			errs = append(errs, fmt.Errorf("%s has %d estimated tax rule files, %v; keep one", dir, len(files), files))
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, files[0]))
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(src), "(def "+estimatedRule(s)+" ") {
			errs = append(errs, fmt.Errorf("returns/rules/states/%s/%s does not define %s", s.Postal, files[0], estimatedRule(s)))
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

// withEstimatedInputs is every state with estimated tax facts of its own, in
// the order given. A file that doesn't declare the state's message is an
// error.
func withEstimatedInputs(root string, ss []State) ([]State, error) {
	var out []State
	var errs []error
	for _, s := range ss {
		src, err := os.ReadFile(filepath.Join(root, estimatedInputsFile(s)))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(src), "\nmessage "+estimatedInputsMessage(s)+" {") {
			errs = append(errs, fmt.Errorf("%s does not declare message %s", estimatedInputsFile(s), estimatedInputsMessage(s)))
			continue
		}
		out = append(out, s)
	}
	return out, errors.Join(errs...)
}

func estimatedDispatch(root string) ([]string, error) {
	ss, err := withEstimatedRules(root)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range ss {
		out = append(out, fmt.Sprintf("    ((is jurisdiction %q) (%s inputs))", strings.ToUpper(s.Postal), estimatedRule(s)))
	}
	return out, nil
}

func estimatedImports(root string) ([]string, error) {
	ss, err := withEstimatedInputs(root, byPostal())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range ss {
		out = append(out, fmt.Sprintf("import %q;", "taxcalcs/"+estimatedInputsFile(s)))
	}
	return out, nil
}

func estimatedProtos(root string) ([]string, error) {
	ss, err := withEstimatedInputs(root, byPostal())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range ss {
		out = append(out, fmt.Sprintf("        %q,", strings.TrimPrefix(estimatedInputsFile(s), "returns/v1/")))
	}
	return out, nil
}

// estimatedOneof is EstimatedStateInputs' oneof, which a state with facts
// of its own joins with its StateInputs number. With no such state there is
// no oneof, since one can't be empty.
func estimatedOneof(root string) ([]string, error) {
	ss, err := withEstimatedInputs(root, byField())
	if err != nil || len(ss) == 0 {
		return nil, err
	}
	out := []string{"  oneof inputs {"}
	for i, s := range ss {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out,
			fmt.Sprintf("    // %s's own facts.", s.Name),
			fmt.Sprintf("    %s %s = %d;", estimatedInputsMessage(s), s.Postal, s.Field))
	}
	return append(out, "  }"), nil
}
