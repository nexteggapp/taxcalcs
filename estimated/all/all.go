// Package all finds a jurisdiction's estimated tax rules for a year.
//
// It is its own package because it imports every jurisdiction, and every
// jurisdiction imports estimated. A caller that wants one jurisdiction's rules
// can import that package and skip this one.
package all

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/alabamataxcalc"
	"github.com/nexteggapp/taxcalcs/alaskataxcalc"
	"github.com/nexteggapp/taxcalcs/arizonataxcalc"
	"github.com/nexteggapp/taxcalcs/arkansastaxcalc"
	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/coloradotaxcalc"
	"github.com/nexteggapp/taxcalcs/connecticuttaxcalc"
	"github.com/nexteggapp/taxcalcs/delawaretaxcalc"
	"github.com/nexteggapp/taxcalcs/districtofcolumbiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/floridataxcalc"
	"github.com/nexteggapp/taxcalcs/georgiataxcalc"
	"github.com/nexteggapp/taxcalcs/hawaiitaxcalc"
	"github.com/nexteggapp/taxcalcs/idahotaxcalc"
	"github.com/nexteggapp/taxcalcs/illinoistaxcalc"
	"github.com/nexteggapp/taxcalcs/indianataxcalc"
	"github.com/nexteggapp/taxcalcs/iowataxcalc"
	"github.com/nexteggapp/taxcalcs/kansastaxcalc"
	"github.com/nexteggapp/taxcalcs/kentuckytaxcalc"
	"github.com/nexteggapp/taxcalcs/louisianataxcalc"
	"github.com/nexteggapp/taxcalcs/mainetaxcalc"
	"github.com/nexteggapp/taxcalcs/marylandtaxcalc"
	"github.com/nexteggapp/taxcalcs/massachusettstaxcalc"
	"github.com/nexteggapp/taxcalcs/michigantaxcalc"
	"github.com/nexteggapp/taxcalcs/minnesotataxcalc"
	"github.com/nexteggapp/taxcalcs/mississippitaxcalc"
	"github.com/nexteggapp/taxcalcs/missouritaxcalc"
	"github.com/nexteggapp/taxcalcs/montanataxcalc"
	"github.com/nexteggapp/taxcalcs/nebraskataxcalc"
	"github.com/nexteggapp/taxcalcs/nevadataxcalc"
	"github.com/nexteggapp/taxcalcs/newhampshiretaxcalc"
	"github.com/nexteggapp/taxcalcs/newjerseytaxcalc"
	"github.com/nexteggapp/taxcalcs/newmexicotaxcalc"
	"github.com/nexteggapp/taxcalcs/newyorktaxcalc"
	"github.com/nexteggapp/taxcalcs/northcarolinataxcalc"
	"github.com/nexteggapp/taxcalcs/northdakotataxcalc"
	"github.com/nexteggapp/taxcalcs/ohiotaxcalc"
	"github.com/nexteggapp/taxcalcs/oklahomataxcalc"
	"github.com/nexteggapp/taxcalcs/oregontaxcalc"
	"github.com/nexteggapp/taxcalcs/pennsylvaniataxcalc"
	"github.com/nexteggapp/taxcalcs/rhodeislandtaxcalc"
	"github.com/nexteggapp/taxcalcs/southcarolinataxcalc"
	"github.com/nexteggapp/taxcalcs/southdakotataxcalc"
	"github.com/nexteggapp/taxcalcs/tennesseetaxcalc"
	"github.com/nexteggapp/taxcalcs/texastaxcalc"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/utahtaxcalc"
	"github.com/nexteggapp/taxcalcs/vermonttaxcalc"
	"github.com/nexteggapp/taxcalcs/virginiataxcalc"
	"github.com/nexteggapp/taxcalcs/washingtontaxcalc"
	"github.com/nexteggapp/taxcalcs/westvirginiataxcalc"
	"github.com/nexteggapp/taxcalcs/wisconsintaxcalc"
	"github.com/nexteggapp/taxcalcs/wyomingtaxcalc"
)

// rules is each jurisdiction's constructor. Every one returns
// [estimated.ErrNoRules] for a year it does not carry.
var rules = map[estimated.Jurisdiction]func(year int) (estimated.EstimatedTax, error){
	estimated.Federal:            ustaxcalc.EstimatedTax,
	estimated.Alabama:            alabamataxcalc.EstimatedTax,
	estimated.Alaska:             alaskataxcalc.EstimatedTax,
	estimated.Arizona:            arizonataxcalc.EstimatedTax,
	estimated.Arkansas:           arkansastaxcalc.EstimatedTax,
	estimated.California:         californiataxcalc.EstimatedTax,
	estimated.Colorado:           coloradotaxcalc.EstimatedTax,
	estimated.Connecticut:        connecticuttaxcalc.EstimatedTax,
	estimated.Delaware:           delawaretaxcalc.EstimatedTax,
	estimated.DistrictOfColumbia: districtofcolumbiataxcalc.EstimatedTax,
	estimated.Florida:            floridataxcalc.EstimatedTax,
	estimated.Georgia:            georgiataxcalc.EstimatedTax,
	estimated.Hawaii:             hawaiitaxcalc.EstimatedTax,
	estimated.Idaho:              idahotaxcalc.EstimatedTax,
	estimated.Illinois:           illinoistaxcalc.EstimatedTax,
	estimated.Indiana:            indianataxcalc.EstimatedTax,
	estimated.Iowa:               iowataxcalc.EstimatedTax,
	estimated.Kansas:             kansastaxcalc.EstimatedTax,
	estimated.Kentucky:           kentuckytaxcalc.EstimatedTax,
	estimated.Louisiana:          louisianataxcalc.EstimatedTax,
	estimated.Maine:              mainetaxcalc.EstimatedTax,
	estimated.Maryland:           marylandtaxcalc.EstimatedTax,
	estimated.Massachusetts:      massachusettstaxcalc.EstimatedTax,
	estimated.Michigan:           michigantaxcalc.EstimatedTax,
	estimated.Minnesota:          minnesotataxcalc.EstimatedTax,
	estimated.Mississippi:        mississippitaxcalc.EstimatedTax,
	estimated.Missouri:           missouritaxcalc.EstimatedTax,
	estimated.Montana:            montanataxcalc.EstimatedTax,
	estimated.Nebraska:           nebraskataxcalc.EstimatedTax,
	estimated.Nevada:             nevadataxcalc.EstimatedTax,
	estimated.NewHampshire:       newhampshiretaxcalc.EstimatedTax,
	estimated.NewJersey:          newjerseytaxcalc.EstimatedTax,
	estimated.NewMexico:          newmexicotaxcalc.EstimatedTax,
	estimated.NewYork:            newyorktaxcalc.EstimatedTax,
	estimated.NorthCarolina:      northcarolinataxcalc.EstimatedTax,
	estimated.NorthDakota:        northdakotataxcalc.EstimatedTax,
	estimated.Ohio:               ohiotaxcalc.EstimatedTax,
	estimated.Oklahoma:           oklahomataxcalc.EstimatedTax,
	estimated.Oregon:             oregontaxcalc.EstimatedTax,
	estimated.Pennsylvania:       pennsylvaniataxcalc.EstimatedTax,
	estimated.RhodeIsland:        rhodeislandtaxcalc.EstimatedTax,
	estimated.SouthCarolina:      southcarolinataxcalc.EstimatedTax,
	estimated.SouthDakota:        southdakotataxcalc.EstimatedTax,
	estimated.Tennessee:          tennesseetaxcalc.EstimatedTax,
	estimated.Texas:              texastaxcalc.EstimatedTax,
	estimated.Utah:               utahtaxcalc.EstimatedTax,
	estimated.Vermont:            vermonttaxcalc.EstimatedTax,
	estimated.Virginia:           virginiataxcalc.EstimatedTax,
	estimated.Washington:         washingtontaxcalc.EstimatedTax,
	estimated.WestVirginia:       westvirginiataxcalc.EstimatedTax,
	estimated.Wisconsin:          wisconsintaxcalc.EstimatedTax,
	estimated.Wyoming:            wyomingtaxcalc.EstimatedTax,
}

// For returns the rules for a jurisdiction and tax year. A jurisdiction with
// no rules built, or a year a built one does not carry, is
// [estimated.ErrNoRules], never the nearest year's rules.
func For(j estimated.Jurisdiction, year int) (estimated.EstimatedTax, error) {
	build, ok := rules[j]
	if !ok {
		return nil, fmt.Errorf("%w: %s", estimated.ErrNoRules, j)
	}
	return build(year)
}
