# Where these files came from

Every Kansas figure in this package is checked against a document the Kansas
Department of Revenue or the Kansas Revisor of Statutes published. These files
are text extracted from them. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext` and
`pdfinfo` from poppler 24.02.0. HTML statute pages were reduced to text with
the Python 3.12.3 script below. Nothing was blocked.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `k-40es-2026.txt` | [K-40ES, Kansas 2026 Individual Estimated Tax](https://www.ksrevenue.gov/pdf/k-40es26.pdf), whole document, reading order | `8ce02ac89717ac81f832629c677fa6cd7f743944d21fe1bb2c6881c529d38490` |
| `k-40es-2026-worksheet.txt` | the same PDF, the Estimated Tax Worksheet, page layout | same |
| `k-210-2025-form.txt` | [Schedule K-210, Kansas Underpayment of Estimated Tax, 2025](https://ksrevenue.gov/pdf/k-21025.pdf), page 1 (the form), page layout | `c7f656eb8d9fb61ac0f2935b917406e9a1e0c87dd1cc91eae88ffb83b0b1dfae` |
| `k-210-2025-instructions.txt` | the same PDF, page 2 (the instructions), reading order | same |
| `form-revisions.txt` | the `Title` metadata of the K-40ES and K-210 PDFs, which carries their revision dates | as above |
| `k-40-2025-lines-18-to-31.txt` | [2025 Kansas Individual Income Tax booklet](https://www.ksrevenue.gov/pdf/ip25.pdf), the instructions from line 18 to line 31, reading order | `8cbc2ea8d4f877a0d57e1e008c203ec06aa087fb3d4ab7319d91c157ee628871` |
| `ksa-79-32-101.txt` | [K.S.A. 79-32,101](https://ksrevisor.gov/statutes/chapters/ch79/079_032_0101.html) | `a498ce612a8deddc7996f73af284a1064aaecacc022562aaa07145119602de18` (the HTML) |
| `ksa-79-32-102.txt` | [K.S.A. 79-32,102](https://ksrevisor.gov/statutes/chapters/ch79/079_032_0102.html) | `0adf30d5b13ee6a310dcb611e3452bdf3a651da052f49f9530b778794fc5a231` (the HTML) |
| `ksa-79-32-103.txt` | [K.S.A. 79-32,103](https://ksrevisor.gov/statutes/chapters/ch79/079_032_0103.html) | `0a18f4db1955a02ad212f509e8e9dcf95de1c2e0b9164a8c65594d78ddeb3826` (the HTML) |
| `ksa-79-32-107.txt` | [K.S.A. 79-32,107](https://ksrevisor.gov/statutes/chapters/ch79/079_032_0107.html) | `5d4ff7e64611e4be1b4135b956af3a8fbb7bcfde81aa80f464a0d30d5b56a9f4` (the HTML) |
| `ksa-79-2968.txt` | [K.S.A. 79-2968](https://ksrevisor.gov/statutes/chapters/ch79/079_029_0068.html) | `26656f3dc87195de2ba175c52bf7df0ee6a7a07eca6928987792a552c14f1d9a` (the HTML) |
| `composite-2026-chapter-79.txt` | [2026 Composite Listing of New, Amended & Repealed Statutes (K.S.A. Order)](https://ksrevisor.gov/rpts/2026NewAmendsAndRepealsKSAOrder.pdf), August 13, 2026, every row for chapter 79 | `0cf22e854d82ef253a976621c7024ee428498b3ecc48989b7f2d66b997452b35` |

An HTML page's hash records what was fetched. The revisor's pages carry
navigation that changes without the statute changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o k-40es26.pdf https://www.ksrevenue.gov/pdf/k-40es26.pdf
curl -o k-21025.pdf  https://ksrevenue.gov/pdf/k-21025.pdf
curl -o ip25.pdf     https://www.ksrevenue.gov/pdf/ip25.pdf
curl -o comp26.pdf   https://ksrevisor.gov/rpts/2026NewAmendsAndRepealsKSAOrder.pdf
R=https://ksrevisor.gov/statutes/chapters/ch79
for s in 101 102 103 107; do curl -o ksa-$s.html "$R/079_032_0$s.html"; done
curl -o ksa-2968.html "$R/079_029_0068.html"

pdftotext k-40es26.pdf k-40es-2026.txt
pdftotext -layout k-40es26.pdf es.L.txt
awk '/ESTIMATED TAX WORKSHEET/{f=1} f; /Enter each payment made in the Estimated Payment Record below\./{if(f)exit}' es.L.txt \
  > k-40es-2026-worksheet.txt

pdftotext -layout -f 1 -l 1 k-21025.pdf k-210-2025-form.txt
pdftotext -f 2 -l 2 k-21025.pdf k-210-2025-instructions.txt
{ pdfinfo k-40es26.pdf | grep '^Title:'; pdfinfo k-21025.pdf | grep '^Title:'; } > form-revisions.txt

pdftotext ip25.pdf ip25.txt
awk '/^LINE 18 \(Total tax balance\)/{f=1} /^LINE 32 \(Amount you owe\)/{if(f)exit} f' ip25.txt \
  > k-40-2025-lines-18-to-31.txt

pdftotext -layout comp26.pdf comp26.txt
grep -E '^ *79-' comp26.txt > composite-2026-chapter-79.txt

cat > html2text.py <<'PY'
# Reduces a ksrevisor.gov statute page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0\r]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
for s in 101 102 103 107; do
  python3 html2text.py ksa-$s.html \
    | awk -v h="79-32,$s." '$0==h{f=1} f; f&&/^History:$/{getline; print; exit}' > ksa-79-32-$s.txt
done
python3 html2text.py ksa-2968.html \
  | awk '$0=="79-2968."{f=1} f; f&&/^History:$/{getline; print; exit}' > ksa-79-2968.txt
```

The K-40ES and the K-210 instructions are set in columns, so they are extracted
in reading order. The K-210 form and the K-40ES worksheet keep their rows only
with their layout. Each statute extract runs from its section number to its
history line.

## What the extracts can and cannot show

There is no worked example with amounts in any Kansas document found. The
strongest checks are Schedule K-210's own: the days it precomputes for each
column (61, 92 and 107 to December 31, and 15 in January), and its example of
an installment paid 13 days late. 2026's due dates fall on the same days of the
year as 2025's, so the 2026 columns run the same spans, and
`TestEstimatedPenaltyColumnsMatchK210Days` requires them.

The forms are followed rather than the statute because neither predates a
change in the law. K.S.A. 79-32,107 was last amended by L. 2024, ch. 81,
effective July 1, 2024. The K-40ES was revised on 8-26-25 and the K-210 on
9-15-25, as their PDF metadata records. The 2026 composite listing has no row
for K.S.A. 79-32,101, 79-32,102, 79-32,103, 79-32,107 or 79-2968.
`TestEstimatedFormsPostdateTheLaw` checks all three. The listing includes the
2024 and 2025 sessions' delayed sections, and the extract keeps every chapter 79
row from all its tables.

## What is not here

- **The 2026 Schedule K-210.** Not published:
  `https://www.ksrevenue.gov/pdf/k-21026.pdf` redirects to the department's 404
  page. The 2025 schedule's arithmetic is used with the 2026 dates.
- **The rate from January 1, 2027.** K.S.A. 79-2968 sets it from the federal
  rate in effect on July 1, 2026, and no Kansas document checked in publishes
  it. The penalty reports the gap in `Required`.
- **The 8% for all of 2026.** The K-210 (2025) charges 8% from January 1,
  2026 to April 15, 2026, and the K-40 booklet charges 8% interest in 2026.
  That it holds to December 31, 2026 rests on K.S.A. 79-2968, which fixes the
  rate once for each calendar year.
- **A Kansas holiday list.** None is checked in. Every 2026 due date the K-40ES
  prints is a weekday, so only weekends are applied.
- **Kansas administrative regulations on short years.** K.S.A. 79-32,101(d)
  leaves short taxable years to regulation, and the research found none
  (`research/estimated-payments/ks.md`, Conflicts resolved).
