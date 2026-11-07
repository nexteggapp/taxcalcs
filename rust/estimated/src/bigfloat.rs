//! The few operations of Go's `math/big.Float` that compound interest uses,
//! at a fixed precision with Go's default rounding, to nearest with ties to
//! even. Every operation computes the exact result and rounds it once, as
//! Go's does, so the figures agree bit for bit.

use num_bigint::{BigInt, BigUint, Sign};
use num_traits::{One, Zero};

/// A binary floating-point number of [`PREC`] bits: (-1)^neg × mant × 2^exp,
/// with `mant` either zero or exactly `PREC` bits long.
#[derive(Clone, Debug)]
pub(crate) struct BigFloat {
    neg: bool,
    mant: BigUint,
    exp: i64,
}

/// The precision compound interest is figured at.
pub(crate) const PREC: u64 = 256;

impl BigFloat {
    /// `v`, exactly.
    pub(crate) fn from_i64(v: i64) -> BigFloat {
        BigFloat::round(v < 0, BigUint::from(v.unsigned_abs()), 0, false)
    }

    /// One half, exactly.
    pub(crate) fn half() -> BigFloat {
        BigFloat::round(false, BigUint::one(), -1, false)
    }

    /// Rounds (-1)^neg × mant × 2^exp to `PREC` bits; `sticky` says that
    /// nonzero bits below `mant` were already dropped.
    fn round(neg: bool, mant: BigUint, exp: i64, sticky: bool) -> BigFloat {
        if mant.is_zero() {
            return BigFloat { neg, mant, exp: 0 };
        }
        let n = mant.bits();
        if n <= PREC {
            let shift = PREC - n;
            return BigFloat {
                neg,
                mant: mant << shift,
                exp: exp - shift as i64,
            };
        }
        let drop = n - PREC;
        let mut keep = &mant >> drop;
        let rem = &mant - (&keep << drop);
        let half = BigUint::one() << (drop - 1);
        let up = rem > half || (rem == half && (sticky || keep.bit(0)));
        let mut exp = exp + drop as i64;
        if up {
            keep += 1u32;
            if keep.bits() > PREC {
                keep >>= 1u32;
                exp += 1;
            }
        }
        BigFloat {
            neg,
            mant: keep,
            exp,
        }
    }

    /// Whether the value is below zero.
    pub(crate) fn is_negative(&self) -> bool {
        self.neg && !self.mant.is_zero()
    }

    /// The value negated.
    pub(crate) fn neg(&self) -> BigFloat {
        BigFloat {
            neg: !self.neg,
            ..self.clone()
        }
    }

    /// The signed exact value as an integer times 2^exp, at the lower of the
    /// two exponents.
    fn aligned(&self, exp: i64) -> BigInt {
        let m = BigInt::from_biguint(Sign::Plus, self.mant.clone()) << (self.exp - exp) as usize;
        if self.neg { -m } else { m }
    }

    /// `self + o`, rounded.
    pub(crate) fn add(&self, o: &BigFloat) -> BigFloat {
        if self.mant.is_zero() {
            return o.clone();
        }
        if o.mant.is_zero() {
            return self.clone();
        }
        let exp = self.exp.min(o.exp);
        let sum = self.aligned(exp) + o.aligned(exp);
        if sum.is_zero() {
            // Go gives +0 for x + (-x) when rounding to nearest.
            return BigFloat {
                neg: false,
                mant: BigUint::zero(),
                exp: 0,
            };
        }
        let (sign, mag) = sum.into_parts();
        BigFloat::round(sign == Sign::Minus, mag, exp, false)
    }

    /// `self - o`, rounded.
    pub(crate) fn sub(&self, o: &BigFloat) -> BigFloat {
        self.add(&o.neg())
    }

    /// `self × o`, rounded.
    pub(crate) fn mul(&self, o: &BigFloat) -> BigFloat {
        BigFloat::round(
            self.neg != o.neg,
            &self.mant * &o.mant,
            self.exp + o.exp,
            false,
        )
    }

    /// `self ÷ o`, rounded; `o` is not zero.
    pub(crate) fn quo(&self, o: &BigFloat) -> BigFloat {
        if self.mant.is_zero() {
            return BigFloat {
                neg: self.neg != o.neg,
                mant: BigUint::zero(),
                exp: 0,
            };
        }
        // Enough quotient bits to round, with the remainder as the sticky
        // bit.
        let shift = (PREC + 2 + o.mant.bits()) as i64 - self.mant.bits() as i64;
        let shift = shift.max(0);
        let num = &self.mant << shift as usize;
        let q = &num / &o.mant;
        let r = num - &q * &o.mant;
        BigFloat::round(self.neg != o.neg, q, self.exp - o.exp - shift, !r.is_zero())
    }

    /// The value truncated toward zero, as Go's `Float.Int`.
    pub(crate) fn int(&self) -> BigInt {
        let m = BigInt::from_biguint(Sign::Plus, self.mant.clone());
        let m = if self.exp >= 0 {
            m << self.exp as usize
        } else {
            m >> (-self.exp) as usize
        };
        if self.neg { -m } else { m }
    }
}
