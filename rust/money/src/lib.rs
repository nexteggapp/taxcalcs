//! The money and rate types every calculator in this workspace uses, ported
//! from the Go package `money`.
//!
//! Nothing here knows about tax. It is a count of cents and a count of basis
//! points, with the arithmetic that turns one into the other. The money type
//! is shared and the tax rules are not: dollars are the same dollars in every
//! jurisdiction, and the rules are different in all of them.
//!
//! The arithmetic is Go's: integer division truncates toward zero, and every
//! rounding rule is the one the Go method states, so a figure computed here
//! matches the Go module's to the cent.

use std::fmt;
use std::iter::Sum;
use std::ops::{Add, AddAssign, Div, Mul, Neg, Rem, Sub, SubAssign};

/// An amount of United States currency, as an integer count of cents.
///
/// Not a float. Every figure here is summed with others, and 0.10 has no
/// exact binary floating point representation.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct USCents(pub i64);

/// One dollar, for writing an amount the way it is spoken:
/// `money::DOLLAR * 42_581`.
pub const DOLLAR: USCents = USCents(100);

/// The amount for a whole number of dollars.
pub fn dollars(d: i64) -> USCents {
    USCents(d * 100)
}

impl USCents {
    /// The count of cents.
    pub fn cents(self) -> i64 {
        self.0
    }

    /// The amount in whole dollars with thousands separators, as a person
    /// reads money: $1,000 or −$1,200 (with U+2212 for the sign). Halves
    /// round away from zero. It is the form for any sentence a person reads;
    /// [`Display`](fmt::Display) keeps the cents for working and logs.
    pub fn whole(self) -> String {
        let (sign, a) = if self.0 < 0 {
            ("\u{2212}", -self.0)
        } else {
            ("", self.0)
        };
        let s = ((a + 50) / 100).to_string();
        let mut out = String::with_capacity(s.len() + s.len() / 3);
        for (i, c) in s.chars().enumerate() {
            if i > 0 && (s.len() - i) % 3 == 0 {
                out.push(',');
            }
            out.push(c);
        }
        format!("{sign}${out}")
    }
}

/// Renders the amount as dollars and cents, without a thousands separator:
/// `$1234.00`, `-$0.50`.
impl fmt::Display for USCents {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let (sign, a) = if self.0 < 0 {
            ("-", -self.0)
        } else {
            ("", self.0)
        };
        write!(f, "{sign}${}.{:02}", a / 100, a % 100)
    }
}

impl Add for USCents {
    type Output = USCents;
    fn add(self, o: USCents) -> USCents {
        USCents(self.0 + o.0)
    }
}

impl Sub for USCents {
    type Output = USCents;
    fn sub(self, o: USCents) -> USCents {
        USCents(self.0 - o.0)
    }
}

impl Neg for USCents {
    type Output = USCents;
    fn neg(self) -> USCents {
        USCents(-self.0)
    }
}

impl AddAssign for USCents {
    fn add_assign(&mut self, o: USCents) {
        self.0 += o.0;
    }
}

impl SubAssign for USCents {
    fn sub_assign(&mut self, o: USCents) {
        self.0 -= o.0;
    }
}

impl Mul<i64> for USCents {
    type Output = USCents;
    fn mul(self, n: i64) -> USCents {
        USCents(self.0 * n)
    }
}

impl Mul<USCents> for i64 {
    type Output = USCents;
    fn mul(self, a: USCents) -> USCents {
        USCents(self * a.0)
    }
}

/// Go's integer division: truncates toward zero.
impl Div<i64> for USCents {
    type Output = USCents;
    fn div(self, n: i64) -> USCents {
        USCents(self.0 / n)
    }
}

/// Go's remainder: takes the sign of the dividend.
impl Rem<i64> for USCents {
    type Output = USCents;
    fn rem(self, n: i64) -> USCents {
        USCents(self.0 % n)
    }
}

impl Sum for USCents {
    fn sum<I: Iterator<Item = USCents>>(iter: I) -> USCents {
        USCents(iter.map(|a| a.0).sum())
    }
}

impl<'a> Sum<&'a USCents> for USCents {
    fn sum<I: Iterator<Item = &'a USCents>>(iter: I) -> USCents {
        USCents(iter.map(|a| a.0).sum())
    }
}

/// A proportion, as an integer count of basis points. One percent is 100
/// basis points, so 22% is 2200 and the 0.9% additional Medicare tax is 90.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct Rate(pub i32);

/// The rate for a whole number of percent.
pub fn percent(p: i32) -> Rate {
    Rate(p * 100)
}

impl Rate {
    /// The count of basis points.
    pub fn basis_points(self) -> i32 {
        self.0
    }

    /// The rate applied to `a`, rounded to the nearest cent with halves
    /// rounded away from zero.
    pub fn of(self, a: USCents) -> USCents {
        let product = a.0 * i64::from(self.0);
        if product < 0 {
            USCents((product - 5000) / 10000)
        } else {
            USCents((product + 5000) / 10000)
        }
    }

    /// The same rate in millionths, which holds it exactly.
    pub fn fine(self) -> FineRate {
        FineRate(i64::from(self.0) * 100)
    }
}

/// Renders the rate as a percentage, trimming a trailing zero so that 2200
/// reads as 22% rather than 22.00%.
impl fmt::Display for Rate {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let (whole, frac) = (self.0 / 100, self.0 % 100);
        if frac == 0 {
            write!(f, "{whole}%")
        } else if frac % 10 == 0 {
            write!(f, "{whole}.{}%", frac / 10)
        } else {
            write!(f, "{whole}.{frac:02}%")
        }
    }
}

/// A proportion as an integer count of millionths. One percent is 10_000 and
/// one basis point is 100, so Maryland's 10.8133% is 108_133.
///
/// It exists for published interest rates that basis points cannot hold.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct FineRate(pub i64);

impl FineRate {
    /// The rate rounded to the nearest basis point, halves away from zero.
    /// It is exact when [`FineRate::is_basis_points`] is true.
    pub fn nearest(self) -> Rate {
        // Go converts with Rate(...), which keeps the low 32 bits.
        let bp = if self.0 < 0 {
            (self.0 - 50) / 100
        } else {
            (self.0 + 50) / 100
        };
        Rate(bp as i32)
    }

    /// Whether the rate is a whole number of basis points.
    pub fn is_basis_points(self) -> bool {
        self.0 % 100 == 0
    }

    /// The rate applied to `a`, rounded to the nearest cent with halves
    /// rounded away from zero, as [`Rate::of`] does.
    pub fn of(self, a: USCents) -> USCents {
        let n = i128::from(a.0) * i128::from(self.0);
        let (mut q, rem) = (n / 1_000_000, n % 1_000_000);
        if rem.abs() >= 500_000 {
            if n < 0 {
                q -= 1;
            } else {
                q += 1;
            }
        }
        // Go's big.Int.Int64 keeps the low 64 bits.
        USCents(q as i64)
    }
}

/// Renders the rate as a percentage with trailing zeros dropped, so that
/// 108_133 reads as 10.8133% and 99_950 as 9.995%.
impl fmt::Display for FineRate {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let (sign, r) = if self.0 < 0 {
            ("-", -self.0)
        } else {
            ("", self.0)
        };
        let (whole, frac) = (r / 10_000, r % 10_000);
        if frac == 0 {
            return write!(f, "{sign}{whole}%");
        }
        let digits = format!("{frac:04}");
        write!(f, "{sign}{whole}.{}%", digits.trim_end_matches('0'))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn formatting() {
        assert_eq!(dollars(1_234).to_string(), "$1234.00");
        assert_eq!(USCents(-50).to_string(), "-$0.50");
        assert_eq!(USCents(19_297_925).to_string(), "$192979.25");
        assert_eq!(percent(22).to_string(), "22%");
        assert_eq!(Rate(90).to_string(), "0.9%");
        assert_eq!(Rate(9235).to_string(), "92.35%");
        assert_eq!(FineRate(108_133).to_string(), "10.8133%");
        assert_eq!(FineRate(99_950).to_string(), "9.995%");
        assert_eq!(USCents(-120_000).whole(), "\u{2212}$1,200");
        assert_eq!(USCents(100_000).whole(), "$1,000");
        assert_eq!(USCents(99_950).whole(), "$1,000");
        assert_eq!(USCents(12_345_678_900).whole(), "$123,456,789");
    }

    #[test]
    fn rounding_halves_away_from_zero() {
        assert_eq!(percent(50).of(USCents(1)), USCents(1));
        assert_eq!(percent(50).of(USCents(-1)), USCents(-1));
        assert_eq!(percent(10).of(USCents(4)), USCents(0));
        assert_eq!(percent(10).of(USCents(5)), USCents(1));
        assert_eq!(FineRate(108_133).nearest(), Rate(1081));
        assert_eq!(FineRate(99_950).nearest(), Rate(1000));
        assert_eq!(FineRate(500_000).of(USCents(1)), USCents(1));
        assert_eq!(FineRate(500_000).of(USCents(-1)), USCents(-1));
    }
}
