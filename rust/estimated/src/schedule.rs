//! Schedules: shares of the year, the annualised recapture, late starts,
//! credits, and applying payments to installments as the penalty forms do.

use money::{Rate, USCents, percent};

use crate::date::{earlier, later};
use crate::interest::{RateSeries, simple_daily};
use crate::output::{Installment, Penalty, Requirement, new_penalty, new_penalty_line};
use crate::{Calendar, Date, Error, Facts, Payment, WaiverKind, invalid};

/// Returns the running total due by each installment when each takes the
/// given share of `annual`.
///
/// Federally the shares are 25% each; California's are 30%, 40%, 0% and
/// 30%. Each running total is computed from `annual` rather than by adding
/// rounded installments, so rounding cannot make the last total miss
/// `annual`. The shares must add to exactly 100%.
pub fn cumulative_shares(annual: USCents, shares: &[Rate]) -> Result<Vec<USCents>, Error> {
    let mut sum = 0i32;
    for s in shares {
        if s.0 < 0 {
            return Err(invalid!("estimated: share {s} is negative"));
        }
        sum = sum.wrapping_add(s.0);
    }
    if Rate(sum) != percent(100) {
        return Err(invalid!("estimated: shares add to {}, not 100%", Rate(sum)));
    }
    let mut running = 0i32;
    Ok(shares
        .iter()
        .map(|s| {
            running = running.wrapping_add(s.0);
            Rate(running).of(annual)
        })
        .collect())
}

/// Turns running totals into the amount each step adds.
pub fn increments(cumulative: &[USCents]) -> Vec<USCents> {
    let mut prev = USCents(0);
    cumulative
        .iter()
        .map(|&c| {
            let step = c - prev;
            prev = c;
            step
        })
        .collect()
}

/// Returns each required installment under the annualised income
/// installment method.
///
/// `annualised_tax` is the tax after credits on each period's income
/// annualised, `applicable` is the percentage of it due by each installment,
/// and `regular` is each installment under the regular method. This is Form
/// 2210 Schedule AI lines 21 to 27, and FTB 5805 Part III lines 16 to 23,
/// which are the same computation with different percentages:
///
///   - the annualised amount is the applicable percentage of the period's
///     tax, less the installments already required, and not below zero;
///   - the regular amount is the regular installment plus whatever the
///     regular method would have required earlier and the annualised method
///     did not;
///   - the installment is the smaller of the two.
///
/// The second rule is the recapture: an installment the annualised method
/// lowered is added back to the next regular amount, so a filer cannot skip
/// it for good.
pub fn annualised(
    annualised_tax: &[USCents],
    applicable: &[Rate],
    regular: &[USCents],
) -> Result<Vec<USCents>, Error> {
    if annualised_tax.len() != applicable.len() || applicable.len() != regular.len() {
        return Err(invalid!(
            "estimated: annualised tax, percentages and regular installments differ in number"
        ));
    }
    let mut out = Vec::with_capacity(regular.len());
    let (mut before, mut carried) = (USCents(0), USCents(0));
    for i in 0..regular.len() {
        let ann = (applicable[i].of(annualised_tax[i]) - before).max(USCents(0));
        let reg = regular[i] + carried;
        let o = ann.min(reg);
        carried = reg - o;
        before += o;
        out.push(o);
    }
    Ok(out)
}

/// The installment a late starter's equal shares begin at: the number of
/// groups, in order, that fall on or before `met`. `groups[i]` is the first
/// day a requirement first met then skips installment `i`, so Georgia's
/// April 1, June 1 and September 1 send a requirement first met on June 1 to
/// the third installment. Zero when `met` is the zero Date.
pub fn late_start(met: Date, groups: &[Date]) -> i64 {
    if met.is_zero() {
        return 0;
    }
    groups.iter().filter(|&&g| !met.before(g)).count() as i64
}

/// Returns the running total due by each of `count` installments when
/// `annual` is paid in equal parts from installment `first` on, as several
/// states have a filer who first meets the requirement late pay the year's
/// amount over the dates that remain.
///
/// The totals before `first` are zero. Each later total is annual × k ÷ the
/// installments remaining, rounded half up, so the last is exactly `annual`
/// and an odd cent falls where the rounding puts it. [`cumulative_shares`]
/// cannot do this, because a third is not a whole number of basis points.
pub fn equal_shares(annual: USCents, count: i64, first: i64) -> Result<Vec<USCents>, Error> {
    if count <= 0 || first < 0 || first >= count {
        return Err(invalid!(
            "estimated: equal shares from installment {} of {}",
            first + 1,
            count
        ));
    }
    if annual.0 < 0 {
        return Err(invalid!("estimated: {annual} cannot be shared out"));
    }
    let n = count - first;
    let mut out = vec![USCents(0); count as usize];
    for k in 1..=n {
        out[(first + k - 1) as usize] = USCents((annual.0 * k * 2 + n) / (2 * n));
    }
    Ok(out)
}

/// Returns each date moved to the next business day.
pub fn move_dates(c: &Calendar, dates: &[Date]) -> Vec<Date> {
    dates.iter().map(|&d| c.next_business_day(d)).collect()
}

/// Lists what counts as paid toward the installments, dated, with
/// withholding deemed paid on the due dates: `shares[i]` of the year's total
/// on `dues[i]`, whenever it was actually withheld. Estimated payments count
/// on their dates. Last year's overpayment applied forward counts on the
/// first due date, as Form 2210, FTB 5805 and REV-1630 all treat it.
///
/// It does not read [`crate::Elections::withholding_as_withheld`]. Whether
/// that election exists is a jurisdiction's rule: federally and in
/// California it does, and the rules call [`credits_as_withheld`] when it is
/// made; Pennsylvania has no such election; Maryland credits withholding on
/// its dates by default.
pub fn credits(f: &Facts, dues: &[Date], shares: &[Rate]) -> Result<Vec<Payment>, Error> {
    if dues.len() != shares.len() || dues.is_empty() {
        return Err(invalid!("estimated: one withholding share per due date"));
    }
    let cum = cumulative_shares(f.withheld_total(), shares)?;
    let mut out = Vec::new();
    for (i, a) in increments(&cum).into_iter().enumerate() {
        if a.0 > 0 {
            out.push(Payment {
                on: dues[i],
                amount: a,
                prior_year: false,
            });
        }
    }
    Ok(with_payments(out, f, dues))
}

/// [`credits`] with withholding counted on the dates it was withheld.
pub fn credits_as_withheld(f: &Facts, dues: &[Date]) -> Result<Vec<Payment>, Error> {
    if dues.is_empty() {
        return Err(invalid!("estimated: no due dates"));
    }
    let out = f
        .withholding
        .iter()
        .map(|w| Payment {
            on: w.on,
            amount: w.amount,
            prior_year: false,
        })
        .collect();
    Ok(with_payments(out, f, dues))
}

fn with_payments(mut out: Vec<Payment>, f: &Facts, dues: &[Date]) -> Vec<Payment> {
    for p in &f.payments {
        let mut p = *p;
        if p.prior_year {
            p.on = dues[0];
        }
        out.push(p);
    }
    out.sort_by_key(|p| p.on.days());
    out
}

/// Part of an installment's shortfall, and the day it was paid.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Piece {
    amount: USCents,
    paid_on: Date,
}

impl Piece {
    /// The amount.
    pub fn amount(&self) -> USCents {
        self.amount
    }

    /// The day the piece was paid, and `None` if it never was.
    pub fn paid_on(&self) -> Option<Date> {
        if self.paid_on.is_zero() {
            None
        } else {
            Some(self.paid_on)
        }
    }
}

/// One installment after the credits have been applied to it.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct LedgerEntry {
    number: i64,
    due: Date,
    required: USCents,
    needed_by: USCents,
    credited_by: USCents,
    shortfall: USCents,
    pieces: Vec<Piece>,
}

impl LedgerEntry {
    /// The installment's position, from 1.
    pub fn number(&self) -> i64 {
        self.number
    }

    /// The due date.
    pub fn due(&self) -> Date {
        self.due
    }

    /// What the installment requires.
    pub fn required(&self) -> USCents {
        self.required
    }

    /// The running total required by the due date.
    pub fn needed_by(&self) -> USCents {
        self.needed_by
    }

    /// The running total credited by the due date.
    pub fn credited_by(&self) -> USCents {
        self.credited_by
    }

    /// The installment's underpayment on its due date.
    pub fn shortfall(&self) -> USCents {
        self.shortfall
    }

    /// The shortfall broken up by the credits that later paid it, in order.
    /// A piece with no date was never paid.
    pub fn pieces(&self) -> Vec<Piece> {
        self.pieces.clone()
    }
}

fn check_dues_and_credits(dues: &[Date], credits: &[Payment]) -> Result<Vec<Payment>, Error> {
    for i in 1..dues.len() {
        if !dues[i].after(dues[i - 1]) {
            return Err(invalid!("estimated: due dates must be in order"));
        }
    }
    let mut cs = credits.to_vec();
    for c in &cs {
        if c.on.is_zero() || c.amount.0 < 0 {
            return Err(invalid!(
                "estimated: every credit needs a date and an amount that is not negative"
            ));
        }
    }
    cs.sort_by_key(|c| c.on.days());
    Ok(cs)
}

/// Applies credits to the required installments in the order Form 2210 and
/// FTB 5805 do.
///
/// A credit pays the oldest unpaid shortfall first, whichever installment it
/// was meant for. What is left over is held toward the next installment.
/// Credits dated on a due date count toward that installment, after paying
/// anything older. This is Form 2210 Part III Section A, lines 10 to 18, and
/// the payment application rule in the instructions for Section B: a credit
/// that pays part of a shortfall splits it into a piece paid that day and a
/// piece still outstanding.
///
/// Installment `i` of the result is for `dues[i]`. Credits after the last
/// due date pay off what is outstanding; any excess is dropped.
pub fn apply(
    dues: &[Date],
    required: &[USCents],
    credits: &[Payment],
) -> Result<Vec<LedgerEntry>, Error> {
    if dues.len() != required.len() || dues.is_empty() {
        return Err(invalid!("estimated: one required amount per due date"));
    }
    let cs = check_dues_and_credits(dues, credits)?;

    let mut out = vec![LedgerEntry::default(); dues.len()];
    let mut pool = USCents(0); // credited and not yet needed
    let (mut credited, mut needed) = (USCents(0), USCents(0));
    fn outstanding(e: &LedgerEntry) -> USCents {
        let paid: USCents = e
            .pieces
            .iter()
            .filter(|p| !p.paid_on.is_zero())
            .map(|p| p.amount)
            .sum();
        e.shortfall - paid
    }
    // pay applies a credit to the oldest outstanding shortfalls among the
    // first n installments and returns what is left of it.
    fn pay(out: &mut [LedgerEntry], n: usize, c: &Payment) -> USCents {
        let mut left = c.amount;
        let mut i = 0;
        while i < n && left.0 > 0 {
            let owed = outstanding(&out[i]);
            if owed.0 > 0 {
                let paid = owed.min(left);
                out[i].pieces.push(Piece {
                    amount: paid,
                    paid_on: c.on,
                });
                left -= paid;
            }
            i += 1;
        }
        left
    }

    let mut next = 0;
    for (i, &due) in dues.iter().enumerate() {
        while next < cs.len() && !cs[next].on.after(due) {
            credited += cs[next].amount;
            pool += pay(&mut out, i, &cs[next]);
            next += 1;
        }
        needed += required[i];
        let covered = pool.min(required[i]);
        pool -= covered;
        out[i] = LedgerEntry {
            number: i as i64 + 1,
            due,
            required: required[i],
            needed_by: needed,
            credited_by: credited,
            shortfall: required[i] - covered,
            pieces: Vec::new(),
        };
    }
    while next < cs.len() {
        pay(&mut out, dues.len(), &cs[next]);
        next += 1;
    }
    for e in &mut out {
        let owed = outstanding(e);
        if owed.0 > 0 {
            e.pieces.push(Piece {
                amount: owed,
                paid_on: Date::default(),
            });
        }
    }
    Ok(out)
}

/// The penalty as simple daily interest on each piece of each shortfall,
/// from the installment's due date to the day the piece was paid or to
/// `stop`, whichever is earlier.
///
/// The returned date, `unrated`, is the first day the series has no rate
/// for, when any piece runs into it, and the zero Date otherwise. Those days
/// are not charged, and the caller must report it.
///
/// `r` and `schedule` are what the penalty was figured from, for its reason.
pub fn simple_penalty(
    ledger: &[LedgerEntry],
    stop: Date,
    s: &RateSeries,
    waiver: WaiverKind,
    r: &Requirement,
    schedule: &[Installment],
) -> Result<(Penalty, Date), Error> {
    let mut pb = new_penalty(stop);
    pb.waiver(waiver).because(r.clone(), schedule);
    let mut unrated = Date::default();
    for a in ledger {
        if a.shortfall.0 <= 0 {
            continue;
        }
        let mut lb = new_penalty_line(a.number, a.due, a.shortfall);
        let mut last = a.due;
        for p in &a.pieces {
            let to = if p.paid_on.is_zero() {
                stop
            } else {
                earlier(p.paid_on, stop)
            };
            if !to.after(a.due) {
                continue;
            }
            last = later(last, to);
            let (parts, gap) = simple_daily(p.amount, a.due, to, s)?;
            if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                unrated = gap;
            }
            for part in parts {
                lb.part(part);
            }
        }
        let line = lb.days(last.days_since(a.due)).build()?;
        pb.line(line);
    }
    if !unrated.is_zero() {
        pb.rated_through(unrated.add_days(-1));
    }
    let p = pb.build()?;
    Ok((p, unrated))
}

/// One installment's column on a penalty form that charges what is short on
/// each due date until it is paid or the next due date comes, as Nebraska's
/// Form 2210N, Missouri's MO-2210 and Mississippi's Form 80-320 do. What is
/// still short at the next due date is that column's to charge.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Column {
    number: i64,
    due: Date,
    end: Date,
    credited: USCents,
    under: USCents,
    pieces: Vec<Piece>,
}

impl Column {
    /// The column's position, from 1.
    pub fn number(&self) -> i64 {
        self.number
    }

    /// The due date.
    pub fn due(&self) -> Date {
        self.due
    }

    /// The next due date, or the last day charged for the final column,
    /// whichever is earlier: the day a piece still unpaid is charged to.
    pub fn end(&self) -> Date {
        self.end
    }

    /// Everything credited on or before the due date.
    pub fn credited_by(&self) -> USCents {
        self.credited
    }

    /// What the running total required by the due date exceeds what was
    /// credited by it. Zero or less is nothing short.
    pub fn under(&self) -> USCents {
        self.under
    }

    /// What was under, split by the credits after the due date and before
    /// [`Column::end`] that paid it, in order. A piece with no date was
    /// still unpaid at the end.
    pub fn pieces(&self) -> Vec<Piece> {
        self.pieces.clone()
    }

    /// The last day charged: the day the last piece was paid, the end if a
    /// piece was never paid, or the due date when nothing was under.
    pub fn through(&self) -> Date {
        let mut last = self.due;
        for p in &self.pieces {
            if p.paid_on.is_zero() {
                return self.end;
            }
            last = p.paid_on;
        }
        last
    }
}

/// Splits each installment's running underpayment into the pieces a
/// column-by-column penalty form charges.
///
/// `cumulative[i]` is the total required by `dues[i]`. A credit counts
/// toward a column when it is dated on or before the due date. Each credit
/// after the due date and before the column's end pays off up to what is
/// left, in date order; what remains runs to the end. A column ends at the
/// next due date, the final one at `last`, and no column ends after `stop`.
/// Columns whose due date is not before `stop` are left out.
///
/// Unlike [`apply`], a credit is not used up by the column it pays: every
/// column measures the credits on its own.
pub fn columns(
    dues: &[Date],
    cumulative: &[USCents],
    credits: &[Payment],
    last: Date,
    stop: Date,
) -> Result<Vec<Column>, Error> {
    if dues.len() != cumulative.len() || dues.is_empty() {
        return Err(invalid!("estimated: one running total per due date"));
    }
    let cs = check_dues_and_credits(dues, credits)?;

    let mut out = Vec::new();
    for (k, &due) in dues.iter().enumerate() {
        if !due.before(stop) {
            break;
        }
        let end = earlier(
            if k + 1 < dues.len() {
                dues[k + 1]
            } else {
                last
            },
            stop,
        );
        let mut col = Column {
            number: k as i64 + 1,
            due,
            end,
            ..Column::default()
        };
        for c in &cs {
            if !c.on.after(due) {
                col.credited += c.amount;
            }
        }
        col.under = cumulative[k] - col.credited;
        let mut left = col.under;
        for c in &cs {
            if left.0 <= 0 || !c.on.after(due) || !c.on.before(end) {
                continue;
            }
            let paid = left.min(c.amount);
            col.pieces.push(Piece {
                amount: paid,
                paid_on: c.on,
            });
            left -= paid;
        }
        if left.0 > 0 {
            col.pieces.push(Piece {
                amount: left,
                paid_on: Date::default(),
            });
        }
        out.push(col);
    }
    Ok(out)
}
