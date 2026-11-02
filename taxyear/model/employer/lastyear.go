package employer

import (
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// LastYearPaycheck is one paycheck at last year's pay: the model's
// last_year W-2 read as a year's pay, as a W-2 for this year is, and shared
// evenly over this year's pay dates. Only gross pay and the pre-tax lines
// are set, so withholding is computed. It is false when the model has no
// last year's W-2 or the year has no pay dates.
func LastYearPaycheck(year int, m *scenariosv1.EmployerModel) (*factsv1.PayFigures, bool) {
	w := m.GetLastYear().GetW2()
	if w == nil {
		return nil, false
	}
	first, err := civil.FromProto(m.GetFirstPayDate())
	if err != nil {
		return nil, false
	}
	starts, err := civil.FromProto(m.GetStarts())
	if err != nil {
		return nil, false
	}
	n := money.USCents(len(PayDates(year, m.GetPayFrequency(), first, starts)))
	if n == 0 {
		return nil, false
	}
	p := w2Pay(w, model.Value(m.GetEmployerHsa()))
	each := pay{gross: setLine(p.gross.v / n), pretax: map[factsv1.PretaxKind]line{}}
	for k, l := range p.pretax {
		if l.set && l.v != 0 {
			each.pretax[k] = setLine(l.v / n)
		}
	}
	return each.proto(), true
}
