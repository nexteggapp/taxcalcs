# Where these files came from

Every Maine figure in this package is checked against one of these files. Five
are statutes, fetched from the Office of the Revisor of Statutes. The sixth is
a labelled copy of quotes from the research file, because Maine Revenue
Services' own documents could not be downloaded.

Retrieved 2026-10-20 with curl 8.5.0. The Revisor's pages say "Data for this
page extracted on 10/05/2026 17:28:28", which each extract keeps. HTML was
reduced to text with the Python 3.12.3 script below, and each extract is
bounded by the section's own heading and its `SECTION HISTORY`.

| File | Source | sha256 of the HTML |
|---|---|---|
| `mrs-36-5228.txt` | [36 M.R.S. § 5228, Estimated tax](https://legislature.maine.gov/statutes/36/title36sec5228.html) | `d16989f1f9f360c0ef83fd7cd85f899741705d378a48c0953d4f576876f86805` |
| `mrs-36-186.txt` | [36 M.R.S. § 186, Interest](https://legislature.maine.gov/statutes/36/title36sec186.html) | `20dba207f514bea656cdd905b472e8ff9f2587052f26f04d4f3c285beba29930` |
| `mrs-36-153.txt` | [36 M.R.S. § 153, Time of filing or paying](https://legislature.maine.gov/statutes/36/title36sec153.html) | `abe56a51580e70ce9c5bd38c1286923946a16e24ab9058ef79fe11ecfd54d249` |
| `mrs-36-5227.txt` | [36 M.R.S. § 5227, Time for filing returns](https://legislature.maine.gov/statutes/36/title36sec5227.html) | `de93ad2a567b364649a97a91d690190b48355369ed25bb9d1f2ed0dc9648de75` |
| `mrs-4-1051.txt` | [4 M.R.S. § 1051, Legal holidays](https://legislature.maine.gov/statutes/4/title4sec1051-1.html) | `07a518b363bb603d308c463f31ca366cd4eafdb0d60d4ce9c3a8cc0d836f675e` |
| `mrs-36-187-B-7.txt` | [36 M.R.S. § 187-B, Penalties](https://legislature.maine.gov/statutes/36/title36sec187-B.html), subsection 7 only | `cdc869ff44e2a2553c7f82a41c37f77ae3a707c5031c2e648c57bc92644fdda0` |
| `research-me-quotes.txt` | quotes copied from [`research/estimated-payments/me.md`](../../research/estimated-payments/me.md) | not fetched |

An HTML page's hash records what was fetched. The page carries navigation that
can change without the section changing, so a later fetch can hash differently
and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
R=https://legislature.maine.gov/statutes
curl -o 5228.html  $R/36/title36sec5228.html
curl -o 186.html   $R/36/title36sec186.html
curl -o 153.html   $R/36/title36sec153.html
curl -o 5227.html  $R/36/title36sec5227.html
curl -o 1051.html  $R/4/title4sec1051-1.html
curl -o 187-B.html $R/36/title36sec187-B.html

cat > html2text.py <<'PY'
# Reduces a Maine Revisor's statute page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0\r]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY

# A section from its heading to its history line, then the extraction date.
section() {
  python3 html2text.py "$1" > full.txt
  awk -v h="$2" '$0==h{f=1} f{print} f && /^SECTION HISTORY$/{getline; print; exit}' full.txt > "$3"
  grep '^Data for this page extracted on' full.txt >> "$3"
}
section 5228.html "§5228. Estimated tax"          mrs-36-5228.txt
section 186.html  "§186. Interest"                mrs-36-186.txt
section 153.html  "§153. Time of filing or paying" mrs-36-153.txt
section 5227.html "§5227. Time for filing returns" mrs-36-5227.txt
section 1051.html "§1051. Legal holidays"         mrs-4-1051.txt

python3 html2text.py 187-B.html > full.txt
awk '/^§187-B\. /{print} /^7\.$/{f=1} f && /^(8\.|SECTION HISTORY)$/{exit} f{print}' full.txt > mrs-36-187-B-7.txt
grep '^Data for this page extracted on' full.txt >> mrs-36-187-B-7.txt
```

§ 1051's page is `title4sec1051-1.html`; `title4sec1051.html` is a 404. The
chapter contents page links the `-1` form.

## Maine Revenue Services' documents were blocked

On 2026-10-20 a plain `curl -L -o` of each of these PDFs, with curl's default
user agent, returned HTTP 403 from www.maine.gov with a CloudFront page reading
"Request blocked.":

- Form 1040ES-ME, 2026: `https://www.maine.gov/revenue/sites/maine.gov.revenue/files/inline-files/26_1040es_fillable.pdf`
- Form 2210ME and instructions, 2025: `.../inline-files/25_form_2210_fillable.pdf`
- Annualized Income Installment Worksheet, 2025: `.../inline-files/25_1040me_form2210_ann_fillable.pdf`
- Form 1040ME General Instructions, 2025: `.../inline-files/25_1040me_gen_instr_w_cover_pg.pdf`

No further attempt was made to get past the block. The figures that only these
documents give are checked instead against the research file's quotes, which
its verification section read on the same day and marks VERIFIED. They are
copied character for character into `research-me-quotes.txt`, which says on
its first line that it is a copy. The test checks each figure against that
file:

- the printed 2026 due dates (Form 1040ES-ME);
- 25% of withholding in each column, line 10a; the $1,000 test as line 1
  less line 3; a short prior year's line 5;
- the 2026 rate of .75% a month, compounded monthly, and a part of a month as
  a whole one, lines 12 and 13;
- the annualized periods and percentages, which the research gives in its own
  words rather than as a quote.

What this cannot check: Form 2210ME's precomputed table of rates, its line
for applying a late payment, and its rounding. The penalty is worked out
exactly and rounded to the cent, a payment is applied to the oldest shortfall
first as Form 2210 does, and the result's Applied rules say so.

## No worked example

Maine's penalty instructions are where a worked example would be, and they
could not be read. The research file reproduces none. The penalty tests are
hand-computed from the quoted rule.

## The rate from January 1, 2027

Not carried. § 186 sets it from the prime rate on September 1, 2026, and no
Maine document read here states it. The penalty reports the months after 2026
in `Required`.
