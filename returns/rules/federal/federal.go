// Package federal loads, typechecks, and evaluates the taxlisp rule modules
// for U.S. federal individual income tax returns (Form 1040 and schedules).
package federal

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"sync"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
)

//go:embed *.tlisp
var ruleFS embed.FS

// Engine holds a parsed, statically typechecked, and initialized taxlisp
// evaluator for the federal Form 1040 rules.
type Engine struct {
	Module    *taxlispv1.ModuleFile
	Checker   *types.Checker
	Evaluator *eval.Evaluator
}

var (
	defaultOnce   sync.Once
	defaultEngine *Engine
	defaultErr    error
)

// DefaultEngine returns a shared, pre-typechecked federal taxlisp Engine.
func DefaultEngine() (*Engine, error) {
	defaultOnce.Do(func() {
		defaultEngine, defaultErr = LoadEngine()
	})
	return defaultEngine, defaultErr
}

// LoadEngine parses all embedded .tlisp files in lexical order, statically
// typechecks the combined module against the protobuf descriptor registry, and
// initializes the runtime evaluator.
func LoadEngine() (*Engine, error) {
	entries, err := fs.ReadDir(ruleFS, ".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded federal rules: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	combined := &taxlispv1.ModuleFile{}
	for _, name := range names {
		raw, err := ruleFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		mod, err := parser.ParseFile(name, string(raw))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		combined.Expressions = append(combined.Expressions, mod.GetExpressions()...)
	}

	checker := types.NewChecker(protoregistry.GlobalFiles)
	if err := checker.CheckModule(combined); err != nil {
		return nil, fmt.Errorf("typechecking federal rules: %w", err)
	}

	ev := eval.NewEvaluator(protoregistry.GlobalFiles, protoregistry.GlobalTypes)
	if _, err := ev.EvalModule(combined); err != nil {
		return nil, fmt.Errorf("initializing federal rules: %w", err)
	}

	return &Engine{
		Module:    combined,
		Checker:   checker,
		Evaluator: ev,
	}, nil
}

// Prepare evaluates `prepare-return` on the given ReturnInputs and returns the
// populated ReturnOutputs message.
func (e *Engine) Prepare(inputs *returnsv1.ReturnInputs) (*returnsv1.ReturnOutputs, error) {
	if inputs == nil {
		inputs = &returnsv1.ReturnInputs{}
	}
	val, err := e.Evaluator.Call("prepare-return", eval.ProtoMessageVal{Msg: inputs.ProtoReflect()})
	if err != nil {
		return nil, err
	}
	pm, ok := val.(eval.ProtoMessageVal)
	if !ok {
		return nil, fmt.Errorf("prepare-return returned %T, want ProtoMessageVal", val)
	}
	out := &returnsv1.ReturnOutputs{}
	b, err := proto.Marshal(pm.Msg.Interface())
	if err != nil {
		return nil, fmt.Errorf("marshaling ReturnOutputs: %w", err)
	}
	if err := proto.Unmarshal(b, out); err != nil {
		return nil, fmt.Errorf("unmarshaling ReturnOutputs: %w", err)
	}
	return out, nil
}

// PrepareFederal evaluates `prepare-federal-return` on the given ReturnInputs
// and returns the populated FederalReturn message.
func (e *Engine) PrepareFederal(inputs *returnsv1.ReturnInputs) (*returnsv1.FederalReturn, error) {
	if inputs == nil {
		inputs = &returnsv1.ReturnInputs{}
	}
	val, err := e.Evaluator.Call("prepare-federal-return", eval.ProtoMessageVal{Msg: inputs.ProtoReflect()})
	if err != nil {
		return nil, err
	}
	pm, ok := val.(eval.ProtoMessageVal)
	if !ok {
		return nil, fmt.Errorf("prepare-federal-return returned %T, want ProtoMessageVal", val)
	}
	out := &returnsv1.FederalReturn{}
	b, err := proto.Marshal(pm.Msg.Interface())
	if err != nil {
		return nil, fmt.Errorf("marshaling FederalReturn: %w", err)
	}
	if err := proto.Unmarshal(b, out); err != nil {
		return nil, fmt.Errorf("unmarshaling FederalReturn: %w", err)
	}
	return out, nil
}
