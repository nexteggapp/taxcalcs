package federal

import (
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

func working(out *returnsv1.ReturnOutputs) []*scenariosv1.Working { return nil }

func (r *Return) diagnostics() {}
