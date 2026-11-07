//! Finds a jurisdiction's estimated tax rules for a year, ported from the
//! Go package `estimated/all`.
//!
//! It is its own crate because it depends on every jurisdiction, and every
//! jurisdiction depends on estimated. A caller that wants one jurisdiction's
//! rules can depend on that crate and skip this one.

use estimated::{Error, EstimatedTax, Jurisdiction};

/// The rules for a jurisdiction and tax year. A jurisdiction with no rules
/// built, or a year a built one does not carry, is [`Error::NoRules`], never
/// the nearest year's rules.
pub fn for_jurisdiction(j: &Jurisdiction, year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match j {
        j if *j == estimated::FEDERAL => ustaxcalc::estimated_tax(year),
        j if *j == estimated::ALABAMA => alabamataxcalc::estimated_tax(year),
        j if *j == estimated::ALASKA => alaskataxcalc::estimated_tax(year),
        j if *j == estimated::ARIZONA => arizonataxcalc::estimated_tax(year),
        j if *j == estimated::ARKANSAS => arkansastaxcalc::estimated_tax(year),
        j if *j == estimated::CALIFORNIA => californiataxcalc::estimated_tax(year),
        j if *j == estimated::COLORADO => coloradotaxcalc::estimated_tax(year),
        j if *j == estimated::CONNECTICUT => connecticuttaxcalc::estimated_tax(year),
        j if *j == estimated::DELAWARE => delawaretaxcalc::estimated_tax(year),
        j if *j == estimated::DISTRICT_OF_COLUMBIA => {
            districtofcolumbiataxcalc::estimated_tax(year)
        }
        j if *j == estimated::FLORIDA => floridataxcalc::estimated_tax(year),
        j if *j == estimated::GEORGIA => georgiataxcalc::estimated_tax(year),
        j if *j == estimated::HAWAII => hawaiitaxcalc::estimated_tax(year),
        j if *j == estimated::IDAHO => idahotaxcalc::estimated_tax(year),
        j if *j == estimated::ILLINOIS => illinoistaxcalc::estimated_tax(year),
        j if *j == estimated::INDIANA => indianataxcalc::estimated_tax(year),
        j if *j == estimated::IOWA => iowataxcalc::estimated_tax(year),
        j if *j == estimated::KANSAS => kansastaxcalc::estimated_tax(year),
        j if *j == estimated::KENTUCKY => kentuckytaxcalc::estimated_tax(year),
        j if *j == estimated::LOUISIANA => louisianataxcalc::estimated_tax(year),
        j if *j == estimated::MAINE => mainetaxcalc::estimated_tax(year),
        j if *j == estimated::MARYLAND => marylandtaxcalc::estimated_tax(year),
        j if *j == estimated::MASSACHUSETTS => massachusettstaxcalc::estimated_tax(year),
        j if *j == estimated::MICHIGAN => michigantaxcalc::estimated_tax(year),
        j if *j == estimated::MINNESOTA => minnesotataxcalc::estimated_tax(year),
        j if *j == estimated::MISSISSIPPI => mississippitaxcalc::estimated_tax(year),
        j if *j == estimated::MISSOURI => missouritaxcalc::estimated_tax(year),
        j if *j == estimated::MONTANA => montanataxcalc::estimated_tax(year),
        j if *j == estimated::NEBRASKA => nebraskataxcalc::estimated_tax(year),
        j if *j == estimated::NEVADA => nevadataxcalc::estimated_tax(year),
        j if *j == estimated::NEW_HAMPSHIRE => newhampshiretaxcalc::estimated_tax(year),
        j if *j == estimated::NEW_JERSEY => newjerseytaxcalc::estimated_tax(year),
        j if *j == estimated::NEW_MEXICO => newmexicotaxcalc::estimated_tax(year),
        j if *j == estimated::NEW_YORK => newyorktaxcalc::estimated_tax(year),
        j if *j == estimated::NORTH_CAROLINA => northcarolinataxcalc::estimated_tax(year),
        j if *j == estimated::NORTH_DAKOTA => northdakotataxcalc::estimated_tax(year),
        j if *j == estimated::OHIO => ohiotaxcalc::estimated_tax(year),
        j if *j == estimated::OKLAHOMA => oklahomataxcalc::estimated_tax(year),
        j if *j == estimated::OREGON => oregontaxcalc::estimated_tax(year),
        j if *j == estimated::PENNSYLVANIA => pennsylvaniataxcalc::estimated_tax(year),
        j if *j == estimated::RHODE_ISLAND => rhodeislandtaxcalc::estimated_tax(year),
        j if *j == estimated::SOUTH_CAROLINA => southcarolinataxcalc::estimated_tax(year),
        j if *j == estimated::SOUTH_DAKOTA => southdakotataxcalc::estimated_tax(year),
        j if *j == estimated::TENNESSEE => tennesseetaxcalc::estimated_tax(year),
        j if *j == estimated::TEXAS => texastaxcalc::estimated_tax(year),
        j if *j == estimated::UTAH => utahtaxcalc::estimated_tax(year),
        j if *j == estimated::VERMONT => vermonttaxcalc::estimated_tax(year),
        j if *j == estimated::VIRGINIA => virginiataxcalc::estimated_tax(year),
        j if *j == estimated::WASHINGTON => washingtontaxcalc::estimated_tax(year),
        j if *j == estimated::WEST_VIRGINIA => westvirginiataxcalc::estimated_tax(year),
        j if *j == estimated::WISCONSIN => wisconsintaxcalc::estimated_tax(year),
        j if *j == estimated::WYOMING => wyomingtaxcalc::estimated_tax(year),
        _ => Err(Error::with_detail(Error::NoRules, j)),
    }
}
