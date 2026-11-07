//! The sequence diff for indexed lists: myers.go.

/// A stretch of a sequence diff between two kept elements: the base
/// elements removed and the new elements added there. next is the base
/// index of the kept element that follows, or n.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct Run {
    pub(crate) removed: Vec<usize>,
    pub(crate) added: Vec<usize>,
    pub(crate) next: usize,
}

/// Diffs a[0:n] against b[0:m] with Myers' greedy algorithm and returns the
/// runs of change between kept elements. It takes O((n+m)·d) time and
/// O(d²) memory for an edit distance of d.
pub(crate) fn myers(n: usize, m: usize, eq: &mut dyn FnMut(usize, usize) -> bool) -> Vec<Run> {
    let (n, m) = (n as i64, m as i64);
    let max = n + m;
    let off = max + 1;
    let mut v = vec![0i64; (2 * max + 3) as usize];
    // trace[d][k+d] is v[k] before step d+1.
    let mut trace: Vec<Vec<i64>> = Vec::new();
    for d in 0..=max {
        let mut k = -d;
        while k <= d {
            let at = |k: i64| (off + k) as usize;
            let mut x = if k == -d || (k != d && v[at(k - 1)] < v[at(k + 1)]) {
                v[at(k + 1)]
            } else {
                v[at(k - 1)] + 1
            };
            let mut y = x - k;
            while x < n && y < m && eq(x as usize, y as usize) {
                x += 1;
                y += 1;
            }
            v[at(k)] = x;
            if x >= n && y >= m {
                trace.push(v[(off - d) as usize..(off + d + 1) as usize].to_vec());
                return runs(&backtrack(&trace, n, m), n as usize);
            }
            k += 2;
        }
        trace.push(v[(off - d) as usize..(off + d + 1) as usize].to_vec());
    }
    // The loop always ends at d = n + m at the latest.
    Vec::new()
}

/// One step of an edit script.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Step {
    /// Keep a[i], which is b[j].
    Keep(usize, usize),
    /// Delete a[i].
    Delete(usize),
    /// Insert b[j].
    Insert(usize),
}

fn backtrack(trace: &[Vec<i64>], n: i64, m: i64) -> Vec<Step> {
    let mut out = Vec::new();
    let (mut x, mut y) = (n, m);
    for d in (0..trace.len() as i64).rev() {
        let k = x - y;
        let prev_k = if d == 0 {
            0
        } else {
            let prev = &trace[(d - 1) as usize];
            let at = |k: i64| prev[(k + d - 1) as usize];
            if k == -d || (k != d && at(k - 1) < at(k + 1)) {
                k + 1
            } else {
                k - 1
            }
        };
        let prev_x = if d > 0 {
            trace[(d - 1) as usize][(prev_k + d - 1) as usize]
        } else {
            0
        };
        let prev_y = prev_x - prev_k;
        while x > prev_x && y > prev_y {
            x -= 1;
            y -= 1;
            out.push(Step::Keep(x as usize, y as usize));
        }
        if d > 0 {
            if x == prev_x {
                y -= 1;
                out.push(Step::Insert(y as usize));
            } else {
                x -= 1;
                out.push(Step::Delete(x as usize));
            }
        }
    }
    out.reverse();
    out
}

fn runs(steps: &[Step], n: usize) -> Vec<Run> {
    let mut out = Vec::new();
    let mut cur = Run::default();
    let mut flush = |cur: &mut Run, next: usize| {
        if !cur.removed.is_empty() || !cur.added.is_empty() {
            cur.next = next;
            out.push(std::mem::take(cur));
        }
        *cur = Run::default();
    };
    for s in steps {
        match *s {
            Step::Keep(i, _) => flush(&mut cur, i),
            Step::Delete(i) => cur.removed.push(i),
            Step::Insert(j) => cur.added.push(j),
        }
    }
    flush(&mut cur, n);
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A small deterministic generator, for the property test below.
    struct Rng(u64);

    impl Rng {
        fn next(&mut self, n: usize) -> usize {
            self.0 ^= self.0 << 13;
            self.0 ^= self.0 >> 7;
            self.0 ^= self.0 << 17;
            (self.0 % n as u64) as usize
        }
    }

    fn lcs_len(a: &[usize], b: &[usize]) -> usize {
        let mut t = vec![vec![0usize; b.len() + 1]; a.len() + 1];
        for i in (0..a.len()).rev() {
            for j in (0..b.len()).rev() {
                t[i][j] = if a[i] == b[j] {
                    t[i + 1][j + 1] + 1
                } else {
                    t[i + 1][j].max(t[i][j + 1])
                };
            }
        }
        t[0][0]
    }

    /// apply's TestMyers: the sequence diff is minimal, against an LCS
    /// table, and its runs cover both sequences.
    #[test]
    fn myers_is_minimal() {
        let mut r = Rng(0x9e37_79b9_7f4a_7c15);
        for _ in 0..3000 {
            let a: Vec<usize> = (0..r.next(12)).map(|_| r.next(4)).collect();
            let b: Vec<usize> = (0..r.next(12)).map(|_| r.next(4)).collect();
            let rs = myers(a.len(), b.len(), &mut |i, j| a[i] == b[j]);
            let (mut removed, mut added) = (0, 0);
            for rn in &rs {
                removed += rn.removed.len();
                added += rn.added.len();
                for &i in &rn.removed {
                    assert!(
                        i < rn.next,
                        "{a:?} -> {b:?}: run {rn:?} removes past its next index"
                    );
                }
            }
            let lcs = lcs_len(&a, &b);
            assert_eq!(
                (removed, added),
                (a.len() - lcs, b.len() - lcs),
                "{a:?} -> {b:?}"
            );
        }
    }

    #[test]
    fn myers_runs() {
        let a = [1, 2, 3, 4];
        let b = [1, 7, 8, 9, 4];
        let rs = myers(a.len(), b.len(), &mut |i, j| a[i] == b[j]);
        assert_eq!(
            rs,
            vec![Run {
                removed: vec![1, 2],
                added: vec![1, 2, 3],
                next: 3
            }]
        );
    }
}
