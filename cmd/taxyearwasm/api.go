// Command taxyearwasm is taxyear for a browser. Built for js/wasm, it
// registers globalThis.taxyear, whose functions take and return binary
// protobuf:
//
//	load()                    loads the rules engine
//	parse(jsonc)              a JSONC file as a TaxYearScenarios
//	format(file)              a TaxYearScenarios as JSON text, for export
//	compute(file, scenario)   a BuilderPage: every result, one view
//	applyEdit(file, edits)    the file with an Edits message applied
//	setWorking(on)            a new cache, with or without the engine's working
//
// Each returns {bytes} or {error}. The cache keeps each scenario's work, so
// compute after an edit recomputes only the scenarios the edit changed.
// measure.mjs times it under node.
package main

import (
	"errors"

	"google.golang.org/protobuf/encoding/protojson"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	"github.com/nexteggapp/taxcalcs/taxyear/federal"
)

// cacheSize is how many scenarios the cache keeps: a file's, twice over,
// so an edit's file and the one before it are both kept.
const cacheSize = 32

// api is the state the JavaScript functions share: one cache.
type api struct {
	cache *taxyear.Cache
}

func newAPI(working bool) *api {
	var opts []taxyear.Option
	if !working {
		opts = append(opts, taxyear.WithoutEngineWorking())
	}
	c, _ := taxyear.NewCache(cacheSize, opts...)
	return &api{cache: c}
}

// load loads the rules engine.
func (a *api) load() error { return federal.Load() }

// parse reads a JSONC file into a binary TaxYearScenarios.
func (a *api) parse(jsonc []byte) ([]byte, error) {
	f, err := taxyear.Parse(jsonc)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(f)
}

// format writes a binary TaxYearScenarios as indented JSON, which parse
// reads back.
func (a *api) format(file []byte) ([]byte, error) {
	f := &scenariosv1.TaxYearScenarios{}
	if err := proto.Unmarshal(file, f); err != nil {
		return nil, err
	}
	return protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(f)
}

// compute computes a binary TaxYearScenarios and returns a binary
// BuilderPage: every scenario's result and one scenario's view.
func (a *api) compute(file []byte, scenario string) ([]byte, error) {
	f := &scenariosv1.TaxYearScenarios{}
	if err := proto.Unmarshal(file, f); err != nil {
		return nil, err
	}
	if scenario == "" {
		scenario = "plan"
	}
	r, err := a.cache.Compute(f)
	if err != nil {
		return nil, err
	}
	v, err := taxyear.BuildView(f, r, scenario)
	if err != nil {
		return nil, err
	}
	return proto.Marshal(&scenariosv1.BuilderPage{Result: r.Message(), View: v})
}

// applyEdit applies a binary Edit, or Edits, to a binary TaxYearScenarios
// and returns the new file.
func (a *api) applyEdit(file, edit []byte) ([]byte, error) {
	f := &scenariosv1.TaxYearScenarios{}
	if err := proto.Unmarshal(file, f); err != nil {
		return nil, err
	}
	e := &scenariosv1.Edits{}
	if err := proto.Unmarshal(edit, e); err != nil {
		return nil, err
	}
	if len(e.GetEdits()) == 0 {
		return nil, errors.New("taxyearwasm: no edit")
	}
	out, err := a.cache.ApplyEdits(f, e.GetEdits())
	if err != nil {
		return nil, err
	}
	return proto.Marshal(out)
}
